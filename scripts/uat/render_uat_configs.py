#!/usr/bin/env python3
"""渲染 UAT 用的 nextpay / center 配置。

安全约束（有意硬失败而不是降级）：
  - Stripe 密钥必须是 sk_test_*。UAT 会真的在 Stripe 建 Checkout Session，用 live key 跑
    等于在生产账号里造对象。
  - Slack webhooks / bot token、邮件、AWS 凭证一律清空或置 dev：本地实例绝不能往生产频道
    发消息、往真实邮箱发信、或用默认凭证链去动云资源。
"""
import os
import secrets
import shutil
import sys

import yaml

UAT = os.environ["UAT_DIR"]
NEXTPAY_REPO = os.environ["NEXTPAY_REPO"]
REPO_ROOT = os.environ["REPO_ROOT"]
DB_PORT = os.environ["DB_PORT"]
REDIS_PORT = os.environ["REDIS_PORT"]
NEXTPAY_PORT = os.environ["NEXTPAY_PORT"]
CENTER_PORT = os.environ["CENTER_PORT"]


def stripe_test_key() -> str:
    """从主仓 center/config.yml 取 Stripe 测试密钥（overleap 渠道用的那把）。"""
    for path in (
        os.path.join(REPO_ROOT, "center", "config.yml"),
        os.path.join(REPO_ROOT, "api", "config.yml"),
    ):
        if not os.path.exists(path):
            continue
        cfg = yaml.safe_load(open(path)) or {}
        key = ((cfg.get("stripe") or {}).get("secret_key") or "").strip()
        if key.startswith("sk_test") and len(key) > 40:
            return key
    sys.exit("找不到可用的 Stripe 测试密钥（需要 center/config.yml 的 stripe.secret_key，sk_test_*）")


def neutralize_outbound(cfg: dict) -> None:
    cfg["slack"] = {"webhooks": {}, "bot_token": ""}
    mail = dict(cfg.get("mail") or {})
    mail["dev_mode"] = True
    cfg["mail"] = mail
    cfg["edm"] = {"provider": "dev"}
    cfg.pop("aws", None)  # 防止 AWS SDK 默认凭证链摸到 ~/.aws/credentials


def render_nextpay(sk: str) -> None:
    cfg = yaml.safe_load(open(os.path.join(NEXTPAY_REPO, "api", "config.yml")))
    cfg["server"] = {"port": int(NEXTPAY_PORT), "domain": f"127.0.0.1:{NEXTPAY_PORT}"}
    cfg["database"] = {
        "dsn": f"root:dev@tcp(127.0.0.1:{DB_PORT})/nextpay?charset=utf8mb4&parseTime=True&loc=Local",
        "debug": False,
    }
    cfg["redis"] = {"addr": f"127.0.0.1:{REDIS_PORT}", "password": "", "db": 3}
    # asynq 有自己的 redis_addr —— 漏掉它会让队列去连默认端口并刷一屏 connection refused
    asynq = dict(cfg.get("asynq") or {})
    asynq["redis_addr"] = f"127.0.0.1:{REDIS_PORT}"
    cfg["asynq"] = asynq
    cfg["stripe"] = {"secret_key": sk, "webhook_secret": os.environ["STRIPE_WEBHOOK_SECRET"]}
    cfg["log"] = {"level": "info", "path": f"{UAT}/logs/nextpay.log", "cloudwatch": False}
    cfg["payment"] = {
        "page_url": f"http://127.0.0.1:{NEXTPAY_PORT}/pay",
        "default_success_url": "https://www.kaitu.io/zh-CN/pay-result",
        "default_cancel_url": "https://www.kaitu.io/zh-CN/purchase",
    }
    neutralize_outbound(cfg)
    yaml.safe_dump(cfg, open(f"{UAT}/nextpay-config.yml", "w"), allow_unicode=True)


def render_center(sk: str) -> None:
    # center/config.yml 是 gitignored 的本地配置（api/CLAUDE.md 的 Test Convention 同一份文件）。
    # 从零合成一份是行不通的（jwt/secret 等必填项拿不到），所以这里硬要求先拷一份过来。
    src = os.path.join(REPO_ROOT, "center", "config.yml")
    if not os.path.exists(src):
        sys.exit(
            f"缺 {src}。先从主仓拷一份：\n"
            f"  mkdir -p {os.path.dirname(src)} && cp <主仓>/center/config.yml {src}\n"
            "（本脚本会就地改写它的 database/redis/nextpay/log 段并掐断对外出口）"
        )
    # 就地改写会毁掉一份 gitignored、无法从 git 恢复的本地配置（真实 slack webhooks /
    # aws 凭证都在里面）。`center` 在主仓是指向 `api` 的符号链接，所以「在主仓跑一次 up」
    # 就足以覆盖它——2026-09-23 真的发生过。先留一份原件，`down` 负责还原。
    bak = src + ".pre-uat.bak"
    if not os.path.exists(bak):
        shutil.copy2(src, bak)
        print(f"已备份原配置到 {bak}（down 时自动还原）")

    cfg = yaml.safe_load(open(src))
    cfg["is_dev"] = True
    cfg["server"] = dict(cfg.get("server") or {})
    cfg["server"]["port"] = int(CENTER_PORT)
    cfg["database"] = {
        "dsn": f"root:dev@tcp(127.0.0.1:{DB_PORT})/kaitu?charset=utf8mb4&parseTime=True&loc=Local"
    }
    cfg["redis"] = {"addr": f"127.0.0.1:{REDIS_PORT}", "password": "", "db": 4}
    asynq = dict(cfg.get("asynq") or {})
    asynq["redis_addr"] = f"127.0.0.1:{REDIS_PORT}"
    cfg["asynq"] = asynq
    cfg["log"] = {"level": "info", "path": f"{UAT}/logs/center.log", "cloudwatch": False}
    cfg["stripe"] = {"secret_key": sk}  # UAT 测试回读 Checkout Session 用
    # 线上 secret_key 走 SSM/env，配置里本就是空的；一次性库里自造一把即可
    if not (cfg.get("secret_key") or "").strip():
        cfg["secret_key"] = secrets.token_hex(32)
    cfg["nextpay"] = {
        "endpoint": f"http://127.0.0.1:{NEXTPAY_PORT}",
        "access_key": os.environ["NEXTPAY_ACCESS_KEY"],
        "webhook_secret": os.environ["NEXTPAY_WEBHOOK_SECRET"],
        "payment_method": "more",
        "timeout": 30,
    }
    neutralize_outbound(cfg)
    yaml.safe_dump(cfg, open(src, "w"), allow_unicode=True)


def main() -> None:
    sk = stripe_test_key()
    render_nextpay(sk)
    render_center(sk)
    print("UAT 配置已渲染（Stripe 测试模式；Slack/邮件/AWS 出口已掐断）")


if __name__ == "__main__":
    main()
