#!/bin/bash
#
# Kaitu Center 一键部署脚本（ALB 排空滚动部署）。移植自 ../meet 的
# scripts/deploy-api-service.sh——哨兵式远程判定、ALB 摘除/回挂、异常出口补救都照搬，
# 改动只在「配置」段与迁移步骤。
#
# 流程：
#   1. 上传新二进制到全部主机并校验 sha256（旧服务继续运行，无停机）
#   2. 数据库迁移：默认只打印命令（center-deploy 规定 DB 操作由人执行）；RUN_MIGRATE=1 才执行
#   3. 暂停等待管理员确认（可检查配置/执行额外操作）
#   4. 逐台：ALB 摘除 → 排空 → 换二进制 + 重启 → 本地健康检查 → ALB 回挂
#          → 等 ALB 判定 healthy，才动下一台
#
# 环境变量：
#   RUN_MIGRATE=1      用新二进制执行迁移（默认只打印命令；有模型改动时由操作者决定）
#   AUTO_CONFIRM=1     跳过所有交互暂停（CI/无人值守用）
#   SKIP_ALB=1         不摘除 ALB target（应急用；会重新引入 502 窗口）
#   DRAIN_SECONDS=15   摘除后等待在途请求收尾的秒数
#   DRY_RUN=1          只打印将要执行的动作，不实际执行（只读探测照常执行）
#   SELF_TEST=1        只跑远程执行层的回归自检后退出，不部署
#   AWS_REGION / TG_NAME  覆盖默认的 ap-northeast-1 / tokyo-alb-tg-kaitu
#
# ⚠️ macOS 自带的是 bash 3.2：不要用关联数组 / ${v,,} 等 bash 4 语法；且变量名后
# 紧跟中文等多字节字符时必须写 ${VAR}——`$VAR（` 会被 3.2 吞进变量名并在 set -u 下报
# 「未绑定的变量」。
set -eo pipefail

set -u

# ================================================
# 配置
# ================================================
APP_NAME=kaitu-center
BINARY_PATH="./release/$APP_NAME"
BASE_PATH="/apps/kaitu"
CENTER_HOSTS=(center-1 center-2)
SERVICE_UNIT=kaitu-center
KEEP_BAK=3   # 部署成功后保留的带时间戳二进制备份数（不含 .prev）

AWS_REGION="${AWS_REGION:-ap-northeast-1}"
TG_NAME="${TG_NAME:-tokyo-alb-tg-kaitu}"
DRAIN_SECONDS="${DRAIN_SECONDS:-15}"
# ⚠️ 是 /healthy（api/route.go）。ALB 的探测路径是 /health 且 matcher 含 404，
# 所以 ALB 的 healthy 只证明端口在听；真正的 200 门禁是这里的本地检查。
HEALTH_PATH=/healthy

# ================================================
# 远程执行：devops 的退出码不可信
# ================================================
# 实测（2026-08-25）：`devops server run-cmd --host center-1 'exit 7'` 本地退出码
# 是 **0**，远程失败只体现在一行日志里。因此 `$?`、`set -e`、`|| { ... }` 对远程
# 命令一律失效——旧版脚本的「健康检查失败即中止滚动」从来没有生效过，反而无论
# 发生什么都会打印「✅ 全部部署完成」。
#
# 唯一可靠的判据是让远程命令自己证明成功：在命令链尾部追加一个只有全链成功才会
# 执行到的哨兵 printf，然后在本地 grep 它。所有远程调用必须走 remote() / remote_ro()。
#
# 哨兵分两段存放，远程侧才拼成完整串——因为 devops 会把**命令原文**回显进自己的
# 日志（`Starting command execution:` / `SSH command:`）。哨兵字面量若整串出现在
# 命令里，本地 grep 就会在回显行上命中，把「命令文本」当成「命令输出」，于是
# `exit 7` 也被判成成功。这个坑由 SELF_TEST 的第 3 项抓出（2026-08-25）。
# nonce 再保证同一次运行的哨兵不会与任何历史输出撞车。
REMOTE_OK_HEAD="ANCOK"
REMOTE_NONCE="$$-$(date +%s)"
REMOTE_OK="${REMOTE_OK_HEAD}-${REMOTE_NONCE}"
REMOTE_OUT=""

# _remote_exec <host> <command…>：真正执行；成功返回 0，输出留在 $REMOTE_OUT。
# 命令串会被包进 { …; }，所以内部可以安全使用 && / ; / 管道。
#
# ⚠️ 命令里禁止用 `exit`：它会终止远程 shell，尾部的哨兵就永远轮不到执行，
# 成功会被误判成失败。要表达成败请用最后一条命令的退出码（例如 `[ "$ok" = 1 ]`）。
#
# ⚠️ devops 给远程输出加 `<时间> [host] Output:` 前缀，所以想取回一个值时，
# 让远程打印带唯一标记的 `KEY=value`，再从整块输出里 sed 出来。
_remote_exec() {
    local host="$1"; shift
    local raw
    raw="$(devops server run-cmd --host "$host" \
        "{ $* ; } && printf '%s-%s\\n' '$REMOTE_OK_HEAD' '$REMOTE_NONCE'" 2>&1 || true)"
    # 先剔掉命令回显行，再判定（与远程侧拼装互为双保险）。
    REMOTE_OUT="$(printf '%s\n' "$raw" \
        | grep -v 'Starting command execution:' | grep -v 'SSH command:' || true)"
    case "$REMOTE_OUT" in
        *"$REMOTE_OK"*) return 0 ;;
        *) return 1 ;;
    esac
}

# remote：会改变状态的远程命令；DRY_RUN 下只打印不执行。
remote() {
    if [ "${DRY_RUN:-}" = "1" ]; then
        local host="$1"; shift
        echo "    [DRY_RUN] $host: $*"
        REMOTE_OUT="$REMOTE_OK"
        return 0
    fi
    _remote_exec "$@"
}

# remote_ro：只读远程命令（读端口 / 读 instance-id / 校验 sha256）。
# DRY_RUN 下**照样执行**——预演的价值就在于真的去验证 AWS 接入、实例映射和端口
# 一致性，空转的预演什么都证明不了。
remote_ro() {
    _remote_exec "$@"
}

# 打印远程输出（去掉哨兵与 devops 自己的日志噪音），用于失败诊断。
dump_remote() {
    printf '%s\n' "$REMOTE_OUT" | grep -v "$REMOTE_OK" | sed 's/^/    | /' || true
}

die() {
    echo ""
    echo "❌ $*"
    exit 1
}

confirm() {
    local prompt="$1"
    if [ "${AUTO_CONFIRM:-}" = "1" ]; then
        echo "(AUTO_CONFIRM=1，跳过确认: $prompt)"
        return 0
    fi
    # `read` 在 EOF（非交互 stdin）返回 1；没有 `|| true` 会被 set -e 当成失败，
    # 在「迁移已跑、二进制还没切」这个最坏的位置中止部署。
    read -r -p "$prompt " _ || true
}

# ================================================
# SELF_TEST=1：验证「远程失败真的会被抓住」
# ================================================
# 这是对本脚本头号缺陷的回归测试。旧版依赖 devops 的退出码，而它恒为 0——健康
# 门禁因此从未生效，部署无论成败都打印「✅ 全部部署完成」。哨兵机制一旦被谁
# 「顺手简化」回 `devops ... || die`，这个自检立刻变红。
#
#   SELF_TEST=1 bash scripts/deploy-api-service.sh
#
if [ "${SELF_TEST:-}" = "1" ]; then
    TEST_HOST="${TEST_HOST:-${CENTER_HOSTS[0]}}"
    fails=0

    echo "自检主机: $TEST_HOST"

    echo -n "  [1/4] devops 退出码确实不可信（前提）… "
    devops server run-cmd --host "$TEST_HOST" 'exit 7' >/dev/null 2>&1
    if [ $? -eq 0 ]; then
        echo "确认：远程 exit 7，devops 仍返回 0"
    else
        echo "⚠️ devops 这次传播了退出码——哨兵机制依然正确，但注释里的前提已变，请复核。"
    fi

    echo -n "  [2/4] remote() 对成功命令返回 0… "
    if remote "$TEST_HOST" "true"; then echo "✅"; else echo "❌"; fails=$((fails+1)); fi

    echo -n "  [3/4] remote() 对失败命令返回非 0… "
    if remote "$TEST_HOST" "exit 7"; then echo "❌ 漏判！"; fails=$((fails+1)); else echo "✅"; fi

    # 健康检查那条命令内部有循环和分支，最容易被写成 `exit 0` 而吃掉哨兵。
    echo -n "  [4/4] 带循环/分支的命令成功时仍能带回哨兵… "
    if remote "$TEST_HOST" "ok=0; for i in 1 2 3; do ok=1; break; done; [ \"\$ok\" = 1 ]"; then
        echo "✅"
    else
        echo "❌ 哨兵被吞（远程命令里是不是用了 exit？）"; fails=$((fails+1))
    fi

    echo ""
    if [ "$fails" -eq 0 ]; then echo "✅ 自检全部通过"; exit 0; else echo "❌ 自检失败 $fails 项"; exit 1; fi
fi

# ================================================
# 前置检查
# ================================================
command -v devops >/dev/null 2>&1 || die "devops 命令未找到，请先安装 devops 工具"
[ -f "$BINARY_PATH" ] || die "二进制文件 $BINARY_PATH 不存在，请先运行 make deploy-api（会先编译）"

USE_ALB=1
if [ "${SKIP_ALB:-}" = "1" ]; then
    USE_ALB=0
    echo "⚠️  SKIP_ALB=1：不摘除 ALB target，重启期间的请求会吃 502。"
fi

TG_ARN=""
TG_PORT=""
HC_INTERVAL=30
HC_HEALTHY=2

if [ "$USE_ALB" = "1" ]; then
    command -v aws >/dev/null 2>&1 || die "aws CLI 未找到。装上，或用 SKIP_ALB=1 明确放弃 ALB 排空。"
    export AWS_PAGER=""

    TG_ARN="$(aws elbv2 describe-target-groups --region "$AWS_REGION" \
        --names "$TG_NAME" --query 'TargetGroups[0].TargetGroupArn' --output text 2>/dev/null || true)"
    [ -n "$TG_ARN" ] && [ "$TG_ARN" != "None" ] \
        || die "找不到 target group ${TG_NAME}（region ${AWS_REGION}）。检查 AWS 凭证，或用 SKIP_ALB=1 跳过。"

    # 健康检查参数在运行时读取：改了 TG 配置，脚本的等待预算自动跟着走。
    read -r TG_PORT HC_INTERVAL HC_HEALTHY <<<"$(aws elbv2 describe-target-groups \
        --region "$AWS_REGION" --target-group-arns "$TG_ARN" \
        --query 'TargetGroups[0].[Port,HealthCheckIntervalSeconds,HealthyThresholdCount]' \
        --output text)"
    echo "ALB: $TG_NAME  port=$TG_PORT  健康检查 ${HC_INTERVAL}s × ${HC_HEALTHY} 次"
fi

# ================================================
# ALB 辅助
# ================================================
# 某个 instance 在 target group 里的状态；未注册返回空。
alb_state() {
    aws elbv2 describe-target-health --region "$AWS_REGION" --target-group-arn "$TG_ARN" \
        --query "TargetHealthDescriptions[?Target.Id=='$1'].TargetHealth.State | [0]" \
        --output text 2>/dev/null | sed 's/^None$//'
}

# 除 $1 之外，至少还有一个 healthy 的 target。
# 这是全脚本最重要的一道闸：把唯一健康的 target 排空 = 100% 停服，比不摘除更糟。
other_target_healthy() {
    local exclude="$1" n
    n="$(aws elbv2 describe-target-health --region "$AWS_REGION" --target-group-arn "$TG_ARN" \
        --query "length(TargetHealthDescriptions[?Target.Id!='$exclude' && TargetHealth.State=='healthy'])" \
        --output text 2>/dev/null || echo 0)"
    [ "${n:-0}" -ge 1 ]
}

alb_deregister() {
    echo "  ALB 摘除 $1 …"
    if [ "${DRY_RUN:-}" = "1" ]; then echo "    [DRY_RUN] deregister-targets $1"; return 0; fi
    aws elbv2 deregister-targets --region "$AWS_REGION" --target-group-arn "$TG_ARN" \
        --targets "Id=$1,Port=$TG_PORT" >/dev/null
    # ALB 在摘除的瞬间就停止派发**新**请求；deregistration_delay 只管在途连接。
    # 所以这里等的是在途请求收尾，不需要（也不该）等满那 300 秒。
    echo "  排空 ${DRAIN_SECONDS}s（等在途请求收尾）…"
    sleep "$DRAIN_SECONDS"
}

alb_register_and_wait() {
    local iid="$1" budget state waited=0
    echo "  ALB 回挂 $iid …"
    if [ "${DRY_RUN:-}" = "1" ]; then echo "    [DRY_RUN] register-targets $iid"; return 0; fi
    aws elbv2 register-targets --region "$AWS_REGION" --target-group-arn "$TG_ARN" \
        --targets "Id=$iid,Port=$TG_PORT" >/dev/null

    budget=$(( HC_INTERVAL * HC_HEALTHY + 60 ))
    echo "  等待 ALB 判定 healthy（预算 ${budget}s）…"
    while [ "$waited" -lt "$budget" ]; do
        state="$(alb_state "$iid" || true)"
        if [ "$state" = "healthy" ]; then
            echo "  ✅ ALB: $iid healthy"
            DRAINED_INSTANCE=""   # 已安全回归，撤销 trap 的补救职责
            return 0
        fi
        sleep 5
        waited=$(( waited + 5 ))
    done
    echo "  ❌ ALB 未在 ${budget}s 内判定 $iid 为 healthy（当前: ${state:-未注册}）"
    return 1
}

# ================================================
# 异常出口补救：绝不能把 target 丢在摘除状态
# ================================================
TMP_DIR="$(mktemp -d)"
DRAINED_INSTANCE=""

# 重入守卫：INT 触发 cleanup → cleanup 里的 exit 又触发 EXIT trap → cleanup 二次
# 进入。模拟运行实测（2026-08-25）后果是「正在回挂」打印两次、register-targets
# 调两次，且第二次读到的 $? 是 0——**一次被 Ctrl-C 打断的部署会以退出码 0 收场**，
# 对 make / CI / 任何链式调用来说和成功完全没有区别。
# 故：退出码由各 trap 显式传入（信号走 128+signo），并且只跑一次。
CLEANUP_DONE=0

cleanup() {
    local rc="$1"
    if [ "$CLEANUP_DONE" = "1" ]; then exit "$rc"; fi
    CLEANUP_DONE=1
    if [ -n "$DRAINED_INSTANCE" ] && [ "$USE_ALB" = "1" ] && [ "${DRY_RUN:-}" != "1" ]; then
        echo ""
        echo "⚠️  部署在 $DRAINED_INSTANCE 摘除状态下退出——正在回挂，避免 ALB 长期只剩一个 target。"
        aws elbv2 register-targets --region "$AWS_REGION" --target-group-arn "$TG_ARN" \
            --targets "Id=$DRAINED_INSTANCE,Port=$TG_PORT" >/dev/null 2>&1 \
            || echo "   ⚠️ 自动回挂失败，请手动执行：aws elbv2 register-targets --region $AWS_REGION --target-group-arn $TG_ARN --targets Id=$DRAINED_INSTANCE,Port=$TG_PORT"
    fi
    rm -rf "$TMP_DIR"
    exit "$rc"
}
trap 'cleanup $?' EXIT
trap 'cleanup 130' INT
trap 'cleanup 143' TERM

# ================================================
# 步骤 1/4: 上传新版本（旧服务继续运行）
# ================================================
echo "================================================"
echo "步骤 1/4: 上传新版本到全部主机（服务不中断）"
echo "================================================"

# 以 kaitu-center.new 名字上传，避免覆盖运行中的二进制（ETXTBSY / 损坏风险）
cp "$BINARY_PATH" "$TMP_DIR/$APP_NAME.new"
if command -v shasum >/dev/null 2>&1; then
    LOCAL_SHA="$(shasum -a 256 "$BINARY_PATH" | awk '{print $1}')"
else
    LOCAL_SHA="$(sha256sum "$BINARY_PATH" | awk '{print $1}')"
fi
echo "本地二进制 sha256: $LOCAL_SHA"

for host in "${CENTER_HOSTS[@]}"; do
    echo ""
    echo "--- 上传到 $host ---"
    if [ "${DRY_RUN:-}" = "1" ]; then
        echo "    [DRY_RUN] upload-file $BASE_PATH/$APP_NAME.new"
    else
        devops server upload-file --host "$host" --remote-path "$BASE_PATH/" "$TMP_DIR/$APP_NAME.new"
    fi
    remote "$host" "chmod +x $BASE_PATH/$APP_NAME.new && sudo chown ubuntu:ubuntu $BASE_PATH/$APP_NAME.new" \
        || { dump_remote; die "$host 上准备新二进制失败。"; }

    # 上传同样走 devops（退出码不可信），所以用 sha256 证明它真的到了、且没截断。
    if [ "${DRY_RUN:-}" != "1" ]; then
        remote_ro "$host" "sha256sum $BASE_PATH/$APP_NAME.new | awk '{print \$1}'" \
            || { dump_remote; die "$host 上校验新二进制失败。"; }
        case "$REMOTE_OUT" in
            *"$LOCAL_SHA"*) echo "  ✅ sha256 一致" ;;
            *) dump_remote; die "$host 上的 $APP_NAME.new 与本地 sha256 不一致（上传失败或被截断）。" ;;
        esac
        # 切换前先在目标机上真跑一次新二进制：架构 / 动态依赖不对会在这里暴露，而不是在重启之后。
        remote_ro "$host" "$BASE_PATH/$APP_NAME.new version | grep -q 'Git提交'" \
            || { dump_remote; die "$host 上的 $APP_NAME.new 无法运行（version 失败），已中止——没有任何主机被重启。"; }
        echo "  ✅ 新二进制可在 $host 上运行"
    fi
done

# 服务端口以线上 config.yml 的 server.port 为准（写死必然漂移）。
remote_ro "${CENTER_HOSTS[0]}" "echo ANC_LISTEN_PORT=\$(awk '/^server:/{f=1;next} f&&/^[^ #]/{f=0} f&&/^ +port:/{print \$2; exit}' $BASE_PATH/config.yml)" \
    || { dump_remote; die "读取 ${CENTER_HOSTS[0]} 的 server.port 失败。"; }
APP_PORT="$(printf '%s' "$REMOTE_OUT" | sed -n 's/.*ANC_LISTEN_PORT=\([0-9][0-9]*\).*/\1/p' | head -1)"
[ -n "$APP_PORT" ] || die "无法从 config.yml 解析 server.port。"

if [ "$USE_ALB" = "1" ] && [ "$APP_PORT" != "$TG_PORT" ]; then
    die "config.yml 的 server.port=$APP_PORT 与 ALB target 端口 $TG_PORT 不一致，拒绝继续。"
fi

HEALTH_URL="http://127.0.0.1:${APP_PORT}${HEALTH_PATH}"
echo ""
echo "健康检查端点: $HEALTH_URL"

# ================================================
# 步骤 2/4: 数据库迁移（旧服务继续运行）
# ================================================
MIGRATE_HOST="${CENTER_HOSTS[0]}"
echo ""
echo "================================================"
echo "步骤 2/4: 数据库迁移（DB 共享，只需在 $MIGRATE_HOST 上用新二进制跑一次）"
echo "================================================"

MIGRATE_CMD="cd $BASE_PATH && ./$APP_NAME.new migrate -c $BASE_PATH/config.yml"
if [ "${RUN_MIGRATE:-}" != "1" ]; then
    echo ""
    echo "默认不执行迁移（DB 操作由人执行）。本次有模型改动时，在切换前手动执行："
    echo "   devops server run-cmd --host $MIGRATE_HOST '$MIGRATE_CMD'"
    echo "或用 RUN_MIGRATE=1 重新运行本脚本。"
else
    echo ""
    echo "将执行: $MIGRATE_CMD   （在 ${MIGRATE_HOST}）"
    confirm "按回车执行迁移（Ctrl-C 中止部署）..."
    remote "$MIGRATE_HOST" "$MIGRATE_CMD" \
        || { dump_remote; die "迁移失败，已中止——没有任何主机被重启，线上仍是旧版本。"; }
    dump_remote
    echo "✅ 迁移完成"
fi

# ================================================
# 步骤 3/4: 暂停等待管理员操作
# ================================================
echo ""
echo "================================================"
echo "步骤 3/4: 部署已暂停，可执行额外操作"
echo "================================================"
echo ""
echo "现在可以："
echo ""
echo "1. 检查配置文件："
for host in "${CENTER_HOSTS[@]}"; do
    echo "   devops server run-cmd --host $host 'cat $BASE_PATH/config.yml'"
done
echo ""
echo "2. 执行其他必要的操作（seed、SQL 等）..."
echo ""
confirm "完成必要操作后，按回车开始滚动切换..."

# ================================================
# 步骤 4/4: 逐台 摘除 → 切换 → 回挂
# ================================================
echo ""
echo "================================================"
echo "步骤 4/4: 逐台滚动切换"
echo "================================================"

rollback_hint() {
    echo ""
    echo "   秒级回滚（在 $1 上换回上一版二进制）："
    echo "   devops server run-cmd --host $1 'cd $BASE_PATH && mv -f $APP_NAME.prev $APP_NAME && sudo systemctl restart $SERVICE_UNIT'"
}

for host in "${CENTER_HOSTS[@]}"; do
    echo ""
    echo "──────── $host ────────"

    INSTANCE_ID=""
    if [ "$USE_ALB" = "1" ]; then
        # 实例 id 由主机自报（IMDSv2），不在脚本里维护 IP↔instance 映射表——
        # 那种表迟早和现实脱节，而脱节的后果是摘错机器。
        remote_ro "$host" "T=\$(curl -sX PUT 'http://169.254.169.254/latest/api/token' -H 'X-aws-ec2-metadata-token-ttl-seconds: 60') && echo ANC_INSTANCE_ID=\$(curl -s -H \"X-aws-ec2-metadata-token: \$T\" http://169.254.169.254/latest/meta-data/instance-id)" \
            || { dump_remote; die "$host 上取 instance-id 失败（IMDSv2 不可达？）。"; }
        INSTANCE_ID="$(printf '%s' "$REMOTE_OUT" | sed -n 's/.*ANC_INSTANCE_ID=\(i-[0-9a-f][0-9a-f]*\).*/\1/p' | head -1)"
        [ -n "$INSTANCE_ID" ] || { dump_remote; die "$host 的 instance-id 解析失败。"; }

        [ -n "$(alb_state "$INSTANCE_ID")" ] \
            || die "$host ($INSTANCE_ID) 不在 target group $TG_NAME 里。请先确认拓扑，或用 SKIP_ALB=1。"

        other_target_healthy "$INSTANCE_ID" \
            || die "除 $host 外没有其它 healthy target——现在摘除它等于全站停服。已中止，未做任何改动。"

        DRAINED_INSTANCE="$INSTANCE_ID"
        alb_deregister "$INSTANCE_ID"
    fi

    echo "  切换二进制并重启 …"
    # 保留上一版为 kaitu-center.prev 以便秒级回滚，另存一份带时间戳的 .bak（沿用主机上的
    # 既有备份命名）；mv 换名为原子操作。install 会重写 unit 并 daemon-reload + enable，不启动服务。
    remote "$host" "cd $BASE_PATH && { cp -f $APP_NAME $APP_NAME.prev 2>/dev/null || true ; } && { cp -f $APP_NAME $APP_NAME.bak.\$(date +%Y%m%d%H%M%S)-pre-deploy 2>/dev/null || true ; } && mv -f $APP_NAME.new $APP_NAME && sudo ./$APP_NAME install -c $BASE_PATH/config.yml && sudo systemctl restart $SERVICE_UNIT" \
        || { dump_remote; echo "❌ $host 切换失败，已停止滚动；其余主机仍运行旧版本。"; rollback_hint "$host"; exit 1; }

    echo "  本地健康检查 $HEALTH_URL …"
    remote "$host" "ok=0; for i in \$(seq 1 20); do sleep 2; if curl -fsS $HEALTH_URL >/dev/null 2>&1; then ok=1; break; fi; done; if [ \"\$ok\" != 1 ]; then echo '--- systemctl status ---'; sudo systemctl status $SERVICE_UNIT --no-pager 2>&1 | tail -20; fi; [ \"\$ok\" = 1 ]" \
        || { dump_remote; echo "❌ $host 健康检查未通过（40s），已停止滚动；其余主机仍运行旧版本。"; rollback_hint "$host"; exit 1; }
    echo "  ✅ 本地健康检查通过"

    # 新版已健康才清理：二进制带时间戳的 .bak 只留最近 $KEEP_BAK 份（每份 ~65 MB，从不清理会无限堆积）。
    # .prev（秒级回滚用）与 config 备份不动。失败不影响部署。
    remote "$host" "cd $BASE_PATH && ls -1t $APP_NAME.bak.* 2>/dev/null | tail -n +$((KEEP_BAK + 1)) | xargs -r rm -f --" \
        || echo "  ⚠️  $host 清理旧 .bak 失败（忽略）"

    if [ "$USE_ALB" = "1" ]; then
        alb_register_and_wait "$INSTANCE_ID" || {
            echo "❌ $host 回挂后未变 healthy，已停止滚动；其余主机仍运行旧版本。"
            echo "   target 已回挂，ALB 会持续探测；若确认是坏版本，按下面回滚。"
            rollback_hint "$host"
            exit 1
        }
    fi

    echo "✅ $host 部署完成"
done

echo ""
echo "================================================"
echo "✅ 全部部署完成！"
echo "================================================"
if [ "$USE_ALB" = "1" ] && [ "${DRY_RUN:-}" != "1" ]; then
    echo ""
    echo "ALB target 最终状态："
    aws elbv2 describe-target-health --region "$AWS_REGION" --target-group-arn "$TG_ARN" \
        --query 'TargetHealthDescriptions[].[Target.Id,TargetHealth.State]' --output text | sed 's/^/  /'
fi
echo ""
echo "服务管理命令（在服务器上执行）："
echo "  查看状态: sudo systemctl status $SERVICE_UNIT"
echo "  查看日志: sudo journalctl -u $SERVICE_UNIT -f"
echo "  秒级回滚: cd $BASE_PATH && mv -f $APP_NAME.prev $APP_NAME && sudo systemctl restart $SERVICE_UNIT"
echo ""
