#!/usr/bin/env bash
# NextPay + 路由器版 UAT 环境：一次性 MariaDB + Redis + 真实 NextPay + 真实 Center。
#
# 为什么需要它：仓库里所有 checkout 测试都替换 createNextpayCheckoutFn，因此从不接触 NextPay
# 的真实 HTTP 契约；路由器版的 4000 行在没有 router 套餐行的环境里等于未执行代码。这个脚本把
# 两者都放到真实进程之间跑一遍。唯一被模拟的外部动作是「Stripe 投递 checkout.session.completed」
# —— NextPay 的结算与出站 webhook、Center 的验签与入账全是真的。
#
# 用法：
#   scripts/uat/nextpay-router-env.sh up       # 起环境（幂等）
#   scripts/uat/nextpay-router-env.sh down     # 全部停掉并删数据目录
#   scripts/uat/nextpay-router-env.sh env      # 打印本次环境变量（供 e2e 脚本 source）
#
# 前置：homebrew 的 mariadb/redis、Go、本机 nextpay 仓库（NEXTPAY_REPO），以及一份含真实
# Stripe **测试** 密钥的 config.yml（默认取 ../center/config.yml 的 stripe.secret_key）。
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
NEXTPAY_REPO="${NEXTPAY_REPO:-$HOME/projects/wordgate/nextpay}"
# 固定用 /tmp 而不是 $TMPDIR：macOS 的 TMPDIR 是每用户长路径，两次调用解析不一致会让
# `env` 子命令指向另一个目录；而且 DB socket 路径必须 ≤103 字符。
UAT_DIR="${UAT_DIR:-/tmp/k2app-nextpay-uat}"
# socket 路径必须 ≤103 字符，否则 mariadbd 直接 Aborting（见 memory: 一次性 mariadb 配方）
DB_SOCK="${DB_SOCK:-/tmp/k2app-uat-db.sock}"
DB_PORT="${DB_PORT:-3307}"        # 3306 常被到 docker-host 的 SSH 隧道占用，别用
REDIS_PORT="${REDIS_PORT:-6380}"
NEXTPAY_PORT="${NEXTPAY_PORT:-5900}"
CENTER_PORT="${CENTER_PORT:-5899}"
STRIPE_WEBHOOK_SECRET="whsec_uat_local_stripe"

# mariadb 客户端：~/.mylogin.cnf 与环境里的 MYSQL_PWD 都会劫持密码，必须全部绕开
my() { MYSQL_TEST_LOGIN_FILE=/nonexistent env -u MYSQL_PWD mariadb --no-defaults -h 127.0.0.1 -P "$DB_PORT" -uroot -pdev "$@"; }

cmd_up() {
  mkdir -p "$UAT_DIR"/{mariadb,redis,logs}

  if ! lsof -nP -iTCP:"$DB_PORT" -sTCP:LISTEN >/dev/null 2>&1; then
    [ -d "$UAT_DIR/mariadb/data/mysql" ] || mariadb-install-db --datadir="$UAT_DIR/mariadb/data" \
      --auth-root-authentication-method=normal > "$UAT_DIR/logs/installdb.log" 2>&1
    nohup mariadbd --datadir="$UAT_DIR/mariadb/data" --port="$DB_PORT" --bind-address=127.0.0.1 \
      --socket="$DB_SOCK" --pid-file="$UAT_DIR/mariadb/mariadb.pid" --max_connections=300 \
      > "$UAT_DIR/logs/mariadbd.log" 2>&1 &
    sleep 6
    # 127.0.0.1 会被反解成 localhost，root@localhost 也必须设密码
    MYSQL_TEST_LOGIN_FILE=/nonexistent env -u MYSQL_PWD mariadb --no-defaults -S "$DB_SOCK" -uroot --skip-password -e "
      ALTER USER 'root'@'localhost' IDENTIFIED BY 'dev';
      CREATE USER IF NOT EXISTS 'root'@'127.0.0.1' IDENTIFIED BY 'dev';
      GRANT ALL ON *.* TO 'root'@'127.0.0.1' WITH GRANT OPTION; FLUSH PRIVILEGES;"
  fi
  my -e "CREATE DATABASE IF NOT EXISTS nextpay; CREATE DATABASE IF NOT EXISTS kaitu;"

  lsof -nP -iTCP:"$REDIS_PORT" -sTCP:LISTEN >/dev/null 2>&1 || {
    nohup redis-server --port "$REDIS_PORT" --bind 127.0.0.1 --dir "$UAT_DIR/redis" --save '' \
      > "$UAT_DIR/logs/redis.log" 2>&1 &
    sleep 2
  }

  # ---- 租户：直接写本地一次性库（生产建租户走 NextPay admin 的 magic-link 流程）----
  if [ ! -f "$UAT_DIR/tenant.env" ]; then
    local ak="ak_uat_$(openssl rand -hex 16)" ws="whsec_uat_$(openssl rand -hex 16)"
    my nextpay -e "SELECT 1" >/dev/null 2>&1 || true
    printf 'NEXTPAY_ACCESS_KEY=%s\nNEXTPAY_WEBHOOK_SECRET=%s\n' "$ak" "$ws" > "$UAT_DIR/tenant.env"
  fi
  # shellcheck disable=SC1090
  source "$UAT_DIR/tenant.env"

  # ---- 配置：Slack / 邮件 / AWS 全部掐断，绝不让本地实例碰生产出口 ----
  UAT_DIR="$UAT_DIR" NEXTPAY_REPO="$NEXTPAY_REPO" REPO_ROOT="$REPO_ROOT" \
  DB_PORT="$DB_PORT" REDIS_PORT="$REDIS_PORT" NEXTPAY_PORT="$NEXTPAY_PORT" CENTER_PORT="$CENTER_PORT" \
  STRIPE_WEBHOOK_SECRET="$STRIPE_WEBHOOK_SECRET" NEXTPAY_ACCESS_KEY="$NEXTPAY_ACCESS_KEY" \
  NEXTPAY_WEBHOOK_SECRET="$NEXTPAY_WEBHOOK_SECRET" \
  python3 "$REPO_ROOT/scripts/uat/render_uat_configs.py"

  # ---- NextPay ----
  (cd "$NEXTPAY_REPO" && go build -o "$UAT_DIR/nextpay-bin" ./cmd/nextpay)
  "$UAT_DIR/nextpay-bin" migrate -c "$UAT_DIR/nextpay-config.yml" >/dev/null
  my nextpay -e "INSERT IGNORE INTO apps (created_at,updated_at,uuid,name,webhook_url,webhook_secret,success_url,cancel_url,access_key,apple_bundle_id,grace_pay_enabled)
    VALUES (NOW(),NOW(),UUID(),'kaitu-uat','http://127.0.0.1:$CENTER_PORT/webhook/nextpay','$NEXTPAY_WEBHOOK_SECRET',
            'https://www.kaitu.io/zh-CN/pay-result','https://www.kaitu.io/zh-CN/purchase','$NEXTPAY_ACCESS_KEY','',0);"
  pgrep -f "$UAT_DIR/nextpay-bin run" >/dev/null || {
    nohup "$UAT_DIR/nextpay-bin" run -c "$UAT_DIR/nextpay-config.yml" > "$UAT_DIR/logs/nextpay-stdout.log" 2>&1 &
    sleep 5
  }
  curl -fsS -m 5 "http://127.0.0.1:$NEXTPAY_PORT/health" >/dev/null || { echo "NextPay 未就绪"; exit 1; }

  # ---- Center ----
  # 空库 migrate 前要有 login_identifies 占位表（空库 1146 绕过）
  my kaitu -e "CREATE TABLE IF NOT EXISTS login_identifies (id bigint unsigned NOT NULL AUTO_INCREMENT PRIMARY KEY, deleted_at datetime NULL);"
  (cd "$REPO_ROOT/api/cmd" && go build -o "$UAT_DIR/kaitu-center" .)
  (cd "$REPO_ROOT/api/cmd" && "$UAT_DIR/kaitu-center" migrate -c "$REPO_ROOT/center/config.yml" >/dev/null)
  my kaitu < "$REPO_ROOT/scripts/uat/seed_plans.sql"
  pgrep -f "$UAT_DIR/kaitu-center start" >/dev/null || {
    nohup "$UAT_DIR/kaitu-center" start -f -c "$REPO_ROOT/center/config.yml" > "$UAT_DIR/logs/center-stdout.log" 2>&1 &
    sleep 8
  }
  curl -fsS -m 5 "http://127.0.0.1:$CENTER_PORT/api/app/config" >/dev/null || { echo "Center 未就绪"; exit 1; }

  echo "UAT 环境就绪：NextPay :$NEXTPAY_PORT · Center :$CENTER_PORT · DB :$DB_PORT · 日志 $UAT_DIR/logs"
  cmd_env
}

cmd_env() {
  # shellcheck disable=SC1090
  source "$UAT_DIR/tenant.env"
  cat <<EOF
export UAT_DIR="$UAT_DIR"
export DB_PORT="$DB_PORT"
export CENTER_URL="http://127.0.0.1:$CENTER_PORT"
export NEXTPAY_URL="http://127.0.0.1:$NEXTPAY_PORT"
export STRIPE_WEBHOOK_SECRET="$STRIPE_WEBHOOK_SECRET"
export NEXTPAY_UAT=1
EOF
}

cmd_down() {
  # 先还原被 render_uat_configs.py 就地改写的本地 center 配置（gitignored，git 恢复不了）。
  local cfg="$REPO_ROOT/center/config.yml"
  if [ -f "$cfg.pre-uat.bak" ]; then
    mv -f "$cfg.pre-uat.bak" "$cfg"
    echo "已还原 $cfg"
  fi
  pkill -f "$UAT_DIR/kaitu-center start" 2>/dev/null || true
  pkill -f "$UAT_DIR/nextpay-bin run" 2>/dev/null || true
  pkill -f "mariadbd --datadir=$UAT_DIR/mariadb/data" 2>/dev/null || true
  redis-cli -p "$REDIS_PORT" shutdown nosave 2>/dev/null || true
  sleep 2
  rm -rf "$UAT_DIR"
  echo "UAT 环境已清理"
}

case "${1:-up}" in
  up) cmd_up ;;
  down) cmd_down ;;
  env) cmd_env ;;
  *) echo "用法: $0 {up|down|env}"; exit 2 ;;
esac
