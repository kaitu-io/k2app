package center

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/viper"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"

	"github.com/kaitu-io/k2app/api/cloudprovider"
)

// worker_cloud_overage.go — AWS Lightsail 超额兜底（cloud sync 每台实例的收尾步骤,
// 见 syncAccount）。只针对 aws_lightsail 共享池实例:AWS 是唯一超过流量配额后继续
// 服务、按 GB 继续计费的厂商(其余厂商流量包用完会自己停机/限速),失控成本敞口只在
// 这里。三层防线,全部以厂商回报的权威用量为准:
//  1. 对账: 厂商用量显著高于节点自报、或节点根本没在计量 → Slack(计量链路坏了)
//  2. 阈值: 80% / 95% → Slack 预警
//  3. 兜底: ≥100% 且实例仍在运行 → StopInstance 自动停机 + Slack
//     (等价于 2026-08-20 事故里的人工 aws lightsail stop-instance 止血)
// 去重以 CloudInstance.TrafficResetAt(每月 1 日 UTC 翻转)为周期身份。

const (
	awsOverageWarn80Ratio = 0.80
	awsOverageWarn95Ratio = 0.95

	// 对账告警条件: 厂商用量须同时超过节点自报的比例阈值与绝对差,避免正常的
	// 采样时间差(CloudWatch 日粒度 vs 节点实时)造成噪音。
	awsReconcileDivergenceRatio = 1.10
	awsReconcileDivergenceBytes = int64(20) << 30 // 20 GiB

	// 节点静默判据: 节点每 60s 上报一次(usageReportIntervalSec),对账用更宽的
	// 30 分钟窗——月度去重的告警不该被一次瞬时抖动烧掉。
	awsReconcileNodeSilentSec = 30 * 60
)

// awsOverageAutostopEnabled: 自动停机开关(config `cloud_instance.aws_overage_autostop`)。
// 默认开——兜底本身就是这个 worker 的存在意义;关掉后 ≥100% 只告警不停机。
func awsOverageAutostopEnabled() bool {
	if viper.IsSet("cloud_instance.aws_overage_autostop") {
		return viper.GetBool("cloud_instance.aws_overage_autostop")
	}
	return true
}

// checkAWSInstanceOverage runs after upsertCloudInstance for one synced AWS
// instance. provider is the account's live provider (used for StopInstance).
// All failure paths are fail-open (log + return): a broken check must never
// break the sync loop, and a missing usage figure reads as "under quota".
func checkAWSInstanceOverage(ctx context.Context, provider cloudprovider.Provider, status *cloudprovider.InstanceStatus) {
	if status.TrafficTotalBytes <= 0 {
		return // no allowance figure — nothing to compare against
	}
	if status.State != "running" {
		return // stopped/pending instance: not serving, not accruing transfer
	}

	var ci CloudInstance
	if err := db.Get().
		Where("provider = ? AND instance_id = ?", cloudprovider.ProviderAWSLightsail, status.InstanceID).
		First(&ci).Error; err != nil {
		log.Errorf(ctx, "[CLOUD] overage check: load instance %s: %v", status.InstanceID, err)
		return
	}
	if isPrivateCloudInstance(ci.ID) {
		return // 专属线路: 配额语义是卖出量,不参与共享池兜底(v1)
	}

	cycleID := ci.TrafficResetAt
	usedGB := float64(status.TrafficUsedBytes) / float64(1<<30)
	totalGB := float64(status.TrafficTotalBytes) / float64(1<<30)
	ratio := float64(status.TrafficUsedBytes) / float64(status.TrafficTotalBytes)

	reconcileAWSNodeUsage(ctx, &ci, status, cycleID)

	switch {
	case ratio >= 1.0:
		if ci.OverageStopSentResetAt == cycleID {
			return
		}
		if awsOverageAutostopEnabled() {
			stopper, ok := provider.(cloudprovider.InstanceStopper)
			if !ok {
				log.Errorf(ctx, "[CLOUD] overage backstop: provider %s cannot stop instances", provider.Name())
				return
			}
			if _, err := stopper.StopInstance(ctx, status.InstanceID); err != nil {
				// Do NOT mark the dedup field — retry on the next sync.
				log.Errorf(ctx, "[CLOUD] overage backstop: stop %s failed: %v", status.InstanceID, err)
				sendCloudSlackNotification(ctx, "AWS Overage STOP FAILED",
					fmt.Sprintf("%s (%s, %s): %.1f/%.1fGB (%.0f%%) — StopInstance failed, will retry next sync",
						status.Name, status.IPAddress, status.Region, usedGB, totalGB, ratio*100))
				return
			}
			sendCloudSlackNotification(ctx, "AWS Overage: instance STOPPED",
				fmt.Sprintf("%s (%s, %s): %.1f/%.1fGB (%.0f%%) — over allowance, instance stopped to halt metered billing. Resets at %s.",
					status.Name, status.IPAddress, status.Region, usedGB, totalGB, ratio*100,
					time.Unix(cycleID, 0).UTC().Format("2006-01-02")))
		} else {
			sendCloudSlackNotification(ctx, "AWS Overage (autostop disabled)",
				fmt.Sprintf("%s (%s, %s): %.1f/%.1fGB (%.0f%%) — over allowance and STILL RUNNING (cloud_instance.aws_overage_autostop=false)",
					status.Name, status.IPAddress, status.Region, usedGB, totalGB, ratio*100))
		}
		markOverageSent(ctx, ci.ID, "overage_stop_sent_reset_at", cycleID)

	case ratio >= awsOverageWarn95Ratio:
		if ci.OverageWarn95SentResetAt == cycleID {
			return
		}
		sendCloudSlackNotification(ctx, "AWS Traffic 95%",
			fmt.Sprintf("%s (%s, %s): %.1f/%.1fGB (%.0f%%) — will be auto-stopped at 100%%",
				status.Name, status.IPAddress, status.Region, usedGB, totalGB, ratio*100))
		markOverageSent(ctx, ci.ID, "overage_warn95_sent_reset_at", cycleID)

	case ratio >= awsOverageWarn80Ratio:
		if ci.OverageWarn80SentResetAt == cycleID {
			return
		}
		sendCloudSlackNotification(ctx, "AWS Traffic 80%",
			fmt.Sprintf("%s (%s, %s): %.1f/%.1fGB (%.0f%%)",
				status.Name, status.IPAddress, status.Region, usedGB, totalGB, ratio*100))
		markOverageSent(ctx, ci.ID, "overage_warn80_sent_reset_at", cycleID)
	}
}

// reconcileAWSNodeUsage compares the provider-authoritative figure against the
// node's self-report and alerts (once per cycle) when the metering link looks
// broken: no node_usages row at all (a serving node that isn't metering), the
// node went silent, or the provider figure runs far ahead of the self-report.
func reconcileAWSNodeUsage(ctx context.Context, ci *CloudInstance, status *cloudprovider.InstanceStatus, cycleID int64) {
	if ci.ReconcileAlertSentResetAt == cycleID {
		return
	}

	now := time.Now().Unix()
	var problem string
	var nu NodeUsage
	err := db.Get().Where("ipv4 = ?", ci.IPAddress).First(&nu).Error
	switch {
	case err != nil:
		problem = "no node_usages row — instance is serving but the sidecar isn't metering"
	case nu.LastReportAt > 0 && now-nu.LastReportAt > awsReconcileNodeSilentSec:
		problem = fmt.Sprintf("node stopped reporting %d min ago", (now-nu.LastReportAt)/60)
	case float64(status.TrafficUsedBytes) > float64(nu.UsedBytes)*awsReconcileDivergenceRatio &&
		status.TrafficUsedBytes-nu.UsedBytes > awsReconcileDivergenceBytes:
		problem = fmt.Sprintf("provider says %.1fGB but node self-reports %.1fGB — metering drift",
			float64(status.TrafficUsedBytes)/float64(1<<30), float64(nu.UsedBytes)/float64(1<<30))
	default:
		return
	}

	sendCloudSlackNotification(ctx, "AWS Metering Reconcile",
		fmt.Sprintf("%s (%s, %s): %s", status.Name, status.IPAddress, status.Region, problem))
	markOverageSent(ctx, ci.ID, "reconcile_alert_sent_reset_at", cycleID)
}

// markOverageSent stamps one per-cycle dedup column (same pattern as the
// private-node traffic warning worker).
func markOverageSent(ctx context.Context, ciID uint64, col string, cycleID int64) {
	if err := db.Get().Model(&CloudInstance{}).Where("id = ?", ciID).
		Update(col, cycleID).Error; err != nil {
		log.Errorf(ctx, "[CLOUD] overage check: persist %s for instance id=%d: %v", col, ciID, err)
	}
}
