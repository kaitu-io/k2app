package center

// PrivateNodeTrafficWarnMeta 专属线路流量预警邮件渲染数据
type PrivateNodeTrafficWarnMeta struct {
	Percent   int
	UsedGB    string
	TotalGB   string
	Region    string
	ResetDate string
}

// privateNodeTrafficWarnTemplate 专属线路本月流量达到 70%/80%/90% 时的预警邮件
var privateNodeTrafficWarnTemplate = EmailTemplate[PrivateNodeTrafficWarnMeta]{
	Subject: "【开途专属线路】本月流量已用 {{.Percent}}%",
	Body: `您好：

您的开途专属线路（{{.Region}}）本月流量已使用 {{.Percent}}%（{{.UsedGB}} / {{.TotalGB}}）。

专属线路达到 100% 后，为避免产生超额费用，路由器将暂停新建连接，海外访问会中断，直至 {{.ResetDate}} 流量重置。

如需更多流量，可升级线路档位或加购第二条专属线路。

—— 开途团队`,
}

// privateNodeTrafficExhaustedTemplate 流量 100% 用尽、线路已暂停时发送,含升级/加购引导。
var privateNodeTrafficExhaustedTemplate = EmailTemplate[PrivateNodeTrafficWarnMeta]{
	Subject: "【开途专属线路】本月流量已用尽，线路已暂停",
	Body: `您好：

您的开途专属线路（{{.Region}}）本月流量已全部用尽（{{.UsedGB}} / {{.TotalGB}}），为避免产生超额费用，线路已自动暂停，海外访问将中断，直至 {{.ResetDate}} 流量重置后自动恢复。

如需立即恢复使用，您可以：
· 升级到更大流量的线路档位
· 加购第二条专属线路

立即升级 / 加购：https://kaitu.io/account

—— 开途团队`,
}
