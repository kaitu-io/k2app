#!/usr/bin/env python3
"""NextPay + 路由器版端到端 UAT（两台真实服务之间跑）。

先 `scripts/uat/nextpay-router-env.sh up`，再 `python3 scripts/uat/nextpay_router_e2e.py`。

覆盖的是"单测覆盖不到"的那部分：
  - Center ↔ NextPay 的真实 HTTP 契约（建单 / confirm / 出站 webhook 验签 / ObjectID 关联）
  - 路由器版完整生命周期 paid → provisioning → ready → shipped → online
  - 下单门（一户一台、服务套餐仅续费、收货必填、预览放行、跨产品归属）
  - 续费（叠加到期日、不建新线路/新台账、post-commit 通知）

唯一被模拟的外部动作是「Stripe 投递 checkout.session.completed」——用 NextPay 自己的
stripe webhook secret 签名，走它真实的 /webhooks/stripe 入口。之后的一切都是真的。
"""
import hashlib
import hmac
import json
import os
import re
import subprocess
import sys
import time
import urllib.error
import urllib.request

UAT_DIR = os.environ["UAT_DIR"]
DB_PORT = os.environ.get("DB_PORT", "3307")
CENTER = os.environ.get("CENTER_URL", "http://127.0.0.1:5899")
NEXTPAY = os.environ.get("NEXTPAY_URL", "http://127.0.0.1:5900")
STRIPE_WH = os.environ.get("STRIPE_WEBHOOK_SECRET", "whsec_uat_local_stripe")
NEXTPAY_REPO = os.environ.get("NEXTPAY_REPO", os.path.expanduser("~/projects/wordgate/nextpay"))
CENTER_LOG = f"{UAT_DIR}/logs/center.log"

FAILURES: list[str] = []
CHECKS = 0


def check(cond: bool, label: str, detail: str = "") -> bool:
    global CHECKS
    CHECKS += 1
    if cond:
        print(f"  ✓ {label}")
        return True
    FAILURES.append(f"{label} — {detail}")
    print(f"  ✗ {label}  {detail}")
    return False


def sql(db: str, stmt: str) -> str:
    """一次性 UAT 库的只读/写查询。~/.mylogin.cnf 和 MYSQL_PWD 都会劫持密码，必须绕开。"""
    env = dict(os.environ, MYSQL_TEST_LOGIN_FILE="/nonexistent")
    env.pop("MYSQL_PWD", None)
    out = subprocess.run(
        ["mariadb", "--no-defaults", "-h", "127.0.0.1", "-P", DB_PORT, "-uroot", "-pdev", "-N", db, "-e", stmt],
        capture_output=True, text=True, env=env, check=True,
    )
    return out.stdout.strip()


def http(method: str, url: str, body=None, token: str = "", basic=None, allow_redirect=True):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(url, data=data, method=method)
    if data:
        req.add_header("Content-Type", "application/json")
    if token:
        req.add_header("Authorization", "Bearer " + token)
    if basic:
        import base64
        req.add_header("Authorization", "Basic " + base64.b64encode(f"{basic[0]}:{basic[1]}".encode()).decode())

    class NoRedirect(urllib.request.HTTPRedirectHandler):
        def redirect_request(self, *a, **k):
            return None

    opener = urllib.request.build_opener() if allow_redirect else urllib.request.build_opener(NoRedirect)
    try:
        r = opener.open(req, timeout=60)
        return r.status, r.read().decode(), dict(r.headers)
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode(), dict(e.headers)


def api(method: str, path: str, body=None, token: str = ""):
    _, raw, _ = http(method, CENTER + path, body, token)
    try:
        return json.loads(raw)
    except json.JSONDecodeError:
        return {"code": -1, "message": raw[:200]}


def login(email: str, udid: str) -> str:
    api("POST", "/api/auth/code", {"email": email, "language": "zh-CN"})
    time.sleep(1.5)
    code = None
    for line in reversed(open(CENTER_LOG, encoding="utf-8", errors="replace").read().splitlines()):
        m = re.search(r"登录验证码是：(\d{6})", line)
        if m and email in line:
            code = m.group(1)
            break
    if not code:
        sys.exit(f"从 {CENTER_LOG} 抓不到 {email} 的验证码（Center 是否 mail.dev_mode?）")
    d = api("POST", "/api/auth/login", {"email": email, "verificationCode": code, "udid": udid})
    tok = (d.get("data") or {}).get("accessToken")
    if not tok:
        sys.exit(f"登录失败: {d}")
    return tok


def pay_order(order_uuid: str, amount: int) -> None:
    """让 NextPay 认为这笔 Stripe session 已付款：投一条真实签名的 checkout.session.completed。

    NextPay 按 stripe_session_id 查自己的订单，因此必须用它真正创建的那个 session id。
    """
    session_id = sql("nextpay", f"SELECT stripe_session_id FROM orders WHERE object_id='{order_uuid}';")
    if not session_id:
        sys.exit(f"NextPay 侧找不到订单 {order_uuid}")
    tpl = json.load(open(f"{NEXTPAY_REPO}/e2e/testdata/checkout_session_completed.json"))
    obj = tpl["data"]["object"]
    obj.update(id=session_id, mode="payment", payment_status="paid", status="complete",
               amount_total=amount, amount_subtotal=amount, currency="usd", metadata={})
    tpl["id"] = f"evt_uat_{int(time.time()*1000)}"
    payload = json.dumps(tpl).encode()
    ts = int(time.time())
    sig = hmac.new(STRIPE_WH.encode(), f"{ts}.".encode() + payload, hashlib.sha256).hexdigest()
    req = urllib.request.Request(NEXTPAY + "/webhooks/stripe", data=payload,
                                 headers={"Content-Type": "application/json",
                                          "Stripe-Signature": f"t={ts},v1={sig}"})
    status = urllib.request.urlopen(req, timeout=60).status
    if status != 200:
        sys.exit(f"NextPay stripe webhook 返回 {status}")
    time.sleep(5)  # 出站 webhook → Center 入账


def create_order(token: str, plan: str, **extra):
    return api("POST", "/api/user/orders", {"plan": plan, **extra}, token)


# ============================================================ 场景

def scenario_app_purchase(token: str) -> str:
    print("\n[1] App 套餐：真实下单 → 真实 Stripe → 真实入账")
    d = create_order(token, "uat-app-1m")
    check(d.get("code") == 0, "下单成功", str(d.get("message")))
    pay_url = (d.get("data") or {}).get("payUrl", "")
    uuid = ((d.get("data") or {}).get("order") or {}).get("uuid", "")
    check(pay_url.startswith("https://checkout.stripe.com/"),
          "payUrl 是 Stripe 托管收银台直链（客户端只 openExternal，故零 OTA 依赖）", pay_url[:60])

    row = sql("kaitu", f"SELECT channel, nextpay_order_id, JSON_UNQUOTE(JSON_EXTRACT(meta,'$.payUrl')) FROM orders WHERE uuid='{uuid}';")
    channel, np_id, meta_pay = (row.split("\t") + ["", "", ""])[:3]
    check(channel == "nextpay", "订单渠道标为 nextpay", channel)
    check(len(np_id) == 36, "nextpay_order_id 落库", np_id)
    check(meta_pay == f"https://www.kaitu.io/api/orders/{uuid}/pay",
          "Meta.payUrl 是耐久 302 链接而非 Stripe 直链", meta_pay)
    obj = sql("nextpay", f"SELECT object_id, amount, currency FROM orders WHERE object_id='{uuid}';")
    check(obj.startswith(uuid) and "\t1999\tusd" in obj, "NextPay 侧 ObjectID/金额/币种一致", obj)

    before = int(sql("kaitu", "SELECT COALESCE(expired_at,0) FROM users WHERE id=(SELECT user_id FROM orders WHERE uuid='%s');" % uuid) or 0)
    pay_order(uuid, 1999)
    paid = sql("kaitu", f"SELECT is_paid FROM orders WHERE uuid='{uuid}';")
    check(paid == "1", "Center 已入账（NextPay 出站 webhook 验签 + ObjectID 关联全链路）", paid)
    after = int(sql("kaitu", "SELECT COALESCE(expired_at,0) FROM users WHERE id=(SELECT user_id FROM orders WHERE uuid='%s');" % uuid) or 0)
    check(after - max(before, int(time.time())) >= 29 * 86400, "会员期延长约 1 个月", f"{before} -> {after}")
    return uuid


def scenario_durable_link(token: str, paid_uuid: str) -> None:
    print("\n[2] 代付邮件的耐久支付链接（302 端点）")
    st, _, h = http("GET", f"{CENTER}/api/orders/{paid_uuid}/pay", allow_redirect=False)
    check(st == 302 and "/pay-result/" in h.get("Location", ""), "已付订单 → 302 到 pay-result", h.get("Location", ""))

    d = create_order(token, "uat-app-1m")
    unpaid = ((d.get("data") or {}).get("order") or {}).get("uuid", "")
    st, _, h = http("GET", f"{CENTER}/api/orders/{unpaid}/pay", allow_redirect=False)
    check(st == 302 and h.get("Location", "").startswith("https://checkout.stripe.com/"),
          "未付订单 → 302 到可用的 Stripe session", h.get("Location", "")[:55])

    st, _, h = http("GET", f"{CENTER}/api/orders/ord-does-not-exist/pay", allow_redirect=False)
    check(st == 302 and h.get("Location", "").endswith("/purchase"),
          "未知 uuid → 302 回 purchase（不 500、不泄漏）", h.get("Location", ""))


def scenario_router_gates(token: str) -> None:
    print("\n[3] 路由器版下单门")
    d = create_order(token, "router-std-1y")
    check(d.get("code") == 422, "硬件套餐缺收货信息 → 拒", str(d.get("code")))
    d = create_order(token, "router-svc-1y")
    check(d.get("code") == 400, "无路由器买服务套餐 → 拒（仅续费）", str(d.get("message")))
    for plan in ("router-std-1y", "router-svc-1y"):
        d = create_order(token, plan, preview=True)
        check(d.get("code") == 0, f"{plan} 预览放行（网站要能报价）", str(d.get("code")))


def scenario_router_lifecycle(token: str) -> None:
    print("\n[4] 路由器版完整生命周期")
    d = create_order(token, "router-std-1y", region="ap-northeast-1",
                     shipping={"name": "张三", "phone": "13800138000", "address": "北京市朝阳区某路1号"})
    check(d.get("code") == 0, "带收货信息的硬件订单成功", str(d.get("message")))
    uuid = ((d.get("data") or {}).get("order") or {}).get("uuid", "")
    ship = sql("kaitu", f"SELECT router_shipping FROM orders WHERE uuid='{uuid}';")
    check(ship.startswith("{") and "13800138000" in ship, "收货信息落独立 JSON 列（非 Meta）", ship[:50])

    pay_order(uuid, 39900)
    check(sql("kaitu", f"SELECT is_paid FROM orders WHERE uuid='{uuid}';") == "1", "路由器订单入账")
    line = sql("kaitu", "SELECT status, region, traffic_total_bytes FROM private_node_subscriptions ORDER BY id DESC LIMIT 1;")
    check(line.split("\t")[0] in ("pending", "provisioning"), "内部专属线路已建（待开通）", line)
    check("ap-northeast-1" in line and "2199023255552" in line, "线路继承下单地区与套餐流量配额", line)
    ful = sql("kaitu", "SELECT stage, hardware_sku, updated_by FROM router_fulfillments ORDER BY id DESC LIMIT 1;")
    check(ful.startswith("paid\tredmi-ax6s"), "发货台账 stage=paid、机型正确", ful)
    check(sql("kaitu", "SELECT status FROM node_operations ORDER BY id DESC LIMIT 1;") == "queued",
          "开机任务进 NodeOperation 队列（Center 不直接建机）")
    ticket = sql("kaitu", "SELECT JSON_UNQUOTE(JSON_EXTRACT(meta,'$.type')), LEFT(content,12) FROM feedback_tickets ORDER BY id DESC LIMIT 1;")
    check(ticket.startswith("router_order"), "onboarding 工单用路由器版类型", ticket)
    check("开途" in ticket and "Kaitu" not in ticket, "中文工单文案用「开途」，无裸词 Kaitu", ticket)

    d = create_order(token, "router-std-1y", shipping={"name": "张三", "phone": "1", "address": "北京"})
    check(d.get("code") == 400, "一户一台：再买硬件套餐被拒", str(d.get("message")))

    # ---- 线路激活走真实节点自注册（会触发 reconcilePrivateIdentity 的台账同步）----
    sub_id = sql("kaitu", "SELECT id FROM private_node_subscriptions ORDER BY id DESC LIMIT 1;")
    claim = sql("kaitu", f"SELECT provision_claim_token FROM private_node_subscriptions WHERE id={sub_id};")
    st, _, _ = http("PUT", f"{CENTER}/slave/nodes/203.0.113.77", {
        "country": "JP", "region": "ap-northeast-1", "name": "uat-private-node",
        "secretToken": "uat-secret-token-123", "ipType": "non_residential",
        "privateClaim": claim, "brands": ["kaitu"],
        "tunnels": [{"domain": "uat-node.kaitu.test", "protocol": "k2v5", "port": 443,
                     "hasTunnel": True, "serverUrl": "k2v5://uat-node.kaitu.test:443"}]})
    check(st == 200, "节点携带 privateClaim 自注册成功", str(st))
    time.sleep(2)
    check(sql("kaitu", f"SELECT status FROM private_node_subscriptions WHERE id={sub_id};") == "active", "线路激活")
    r = (api("GET", "/api/user/router", token=token).get("data") or {})
    check((r.get("fulfillment") or {}).get("stage") == "ready",
          "节点自注册把台账推进到 ready（reconcilePrivateIdentity 同步分支）",
          str((r.get("fulfillment") or {}).get("stage")))
    check((r.get("fulfillment") or {}).get("canMintCredential") is True, "线路可服务 → 允许铸凭证")
    check(r.get("renewPlanPid") == "router-svc-1y", "账户页给出续费套餐 pid", str(r.get("renewPlanPid")))


def scenario_admin_and_online(token: str) -> None:
    """后台台账 + 真实生产顺序：铸凭证烧录 → 仓库测试连接 → 发货 → 客户上电 → online。

    顺序是载荷的一部分：技术员先铸凭证并在仓库跑一次 k2r setup（这会打到 /api/subs 并记下活动），
    之后才发货。若上线门槛只比"铸造时刻"，刚贴单的路由器就会在快递途中显示 online。
    """
    print("\n[5] 后台台账 / 铸凭证 / 发货 / 上线判定")
    fid = sql("kaitu", "SELECT id FROM router_fulfillments ORDER BY id DESC LIMIT 1;")
    d = api("GET", "/app/router/fulfillments", token=token)
    items = (d.get("data") or {}).get("items") or []
    check(d.get("code") == 0 and bool(items), "后台台账列表可读", str(d.get("code")))
    if items:
        check(isinstance(items[0].get("shipping"), dict), "台账带解析后的收货信息", str(items[0].get("shipping")))
        check(bool(items[0].get("email")), "台账带归属用户邮箱")
    stats = (api("GET", "/app/router/stats", token=token).get("data") or {})
    check(len(stats.get("stageCounts") or {}) == 6, "看板六个 stage 键齐全（含计数 0）", str(stats.get("stageCounts")))

    # ---- 1) 线路 ready 时铸凭证（烧录进成品）----
    d = api("POST", "/api/user/gateway-credential", {}, token)
    url = (d.get("data") or {}).get("url", "")
    check(url.startswith("k2subs://"), "铸出 k2subs 凭证", url[:22])
    row = sql("kaitu", f"SELECT gateway_device_id IS NOT NULL, credential_minted_at>0 FROM router_fulfillments WHERE id={fid};")
    check(row == "1\t1", "凭证挂到台账（device id + 铸造时刻）", row)
    m = re.match(r"k2subs://([^:]+):([^@]+)@", url)
    creds = (m.group(1), m.group(2))

    # ---- 2) 仓库内测试连接：成品不得因此跳过发货直接 online ----
    st, body, _ = http("GET", f"{CENTER}/api/subs", basic=creds)
    check(st == 200 and "k2v5://" in body, "路由器拉到专属隧道订阅", f"HTTP {st}")
    stage = ((api("GET", "/api/user/router", token=token).get("data") or {}).get("fulfillment") or {}).get("stage")
    check(stage == "ready", "仓库内烧录测试不会让成品跳过发货直接 online", str(stage))

    # ---- 3) 阶段门 ----
    d = api("POST", f"/app/router/fulfillments/{fid}/stage", {"stage": "shipped"}, token)
    check(d.get("code") == 422, "标发货不填快递单号 → 拒", str(d.get("message")))
    d = api("POST", f"/app/router/fulfillments/{fid}/stage", {"stage": "online"}, token)
    check(d.get("code") == 400, "自动阶段不可手动设置 → 拒", str(d.get("message")))

    # ---- 4) 发货。此刻 shipped_at 晚于仓库活动 → 在途不得判 online ----
    time.sleep(1.1)  # 跨过秒边界：上线判定是「严格晚于」，同秒不算
    d = api("POST", f"/app/router/fulfillments/{fid}/stage",
            {"stage": "shipped", "trackingNo": "SF1234567890", "carrier": "顺丰"}, token)
    check(d.get("code") == 0, "带单号标发货成功", str(d.get("message")))
    f = ((api("GET", "/api/user/router", token=token).get("data") or {}).get("fulfillment") or {})
    check(f.get("stage") == "shipped", "关键防误判：在途（发货后尚未再连接）不得判 online", str(f.get("stage")))
    check(int(f.get("activatedAt") or 0) == 0, "在途时 activatedAt 仍为 0", str(f.get("activatedAt")))

    # ---- 5) 客户上电，路由器再次拉订阅 → online ----
    time.sleep(1.1)
    st, _, _ = http("GET", f"{CENTER}/api/subs", basic=creds)
    check(st == 200, "客户侧路由器拉订阅成功", f"HTTP {st}")
    time.sleep(1)
    r = (api("GET", "/api/user/router", token=token).get("data") or {})
    f = r.get("fulfillment") or {}
    check(f.get("stage") == "online", "客户上电后推进到 online", str(f.get("stage")))
    shipped_at = int(sql("kaitu", f"SELECT shipped_at FROM router_fulfillments WHERE id={fid};"))
    check(int(f.get("activatedAt") or 0) > shipped_at,
          "上线时刻严格晚于发货时刻", f"{f.get('activatedAt')} > {shipped_at}")
    check((r.get("device") or {}).get("online") is True, "设备在 65 分钟窗口内判为在线")


def scenario_renewal(token: str) -> None:
    print("\n[6] 续费")
    sub_id = sql("kaitu", "SELECT id FROM private_node_subscriptions ORDER BY id DESC LIMIT 1;")
    before = int(sql("kaitu", f"SELECT expires_at FROM private_node_subscriptions WHERE id={sub_id};"))
    n_subs = sql("kaitu", "SELECT COUNT(*) FROM private_node_subscriptions;")
    n_ful = sql("kaitu", "SELECT COUNT(*) FROM router_fulfillments;")
    slack_before = open(CENTER_LOG, encoding="utf-8", errors="replace").read().count("Failed to send Slack notification")

    d = create_order(token, "router-svc-1y")
    check(d.get("code") == 0, "有路由器后服务套餐放行", str(d.get("message")))
    uuid = ((d.get("data") or {}).get("order") or {}).get("uuid", "")
    pay_order(uuid, 29900)

    after = int(sql("kaitu", f"SELECT expires_at FROM private_node_subscriptions WHERE id={sub_id};"))
    check(360 <= (after - before) // 86400 <= 366, "到期日从原到期日叠加 12 个月", f"+{(after-before)//86400}d")
    check(sql("kaitu", "SELECT COUNT(*) FROM private_node_subscriptions;") == n_subs, "续费不建新线路")
    check(sql("kaitu", "SELECT COUNT(*) FROM router_fulfillments;") == n_ful, "续费不建新台账")
    slack_after = open(CENTER_LOG, encoding="utf-8", errors="replace").read().count("Failed to send Slack notification")
    check(slack_after > slack_before,
          "续费通知在事务提交后触发（webhook 未配置，故每次触发留一条 error 计数）",
          f"{slack_before} -> {slack_after}")


def scenario_cross_product(_token: str) -> None:
    print("\n[7] 跨产品归属：定制线路客户不是路由器车主")
    email = f"uat-pnonly-{int(time.time())}@kaitu.test"
    tok = login(email, f"uat-dev-pn-{int(time.time())}")
    uid = sql("kaitu", f"""SELECT u.id FROM users u JOIN login_identifies l ON l.user_id=u.id
                           WHERE l.index_id IS NOT NULL ORDER BY u.id DESC LIMIT 1;""")
    # 造一个纯定制线路老客户：活跃 private_node 线路，零路由器台账（= 线上现存的那一种形态）
    sql("kaitu", f"""INSERT INTO plans (created_at,updated_at,pid,label,price,origin_price,month,highlight,is_active,tier,product,hardware_sku,brand)
                     VALUES (NOW(),NOW(),'pn-uat-{int(time.time())%100000}','定制线路 2T',59900,59900,12,0,0,'pro','private_node','','kaitu');""")
    pn_plan = sql("kaitu", "SELECT id FROM plans WHERE product='private_node' ORDER BY id DESC LIMIT 1;")
    sql("kaitu", f"""INSERT INTO private_node_subscriptions
        (user_id,order_id,plan_id,status,region,ip_type,traffic_total_bytes,purchased_at,expires_at,grace_until,suspend_until,provision_claim_token,created_at,updated_at)
        VALUES ({uid},900000{int(time.time())%1000},{pn_plan},'active','ap-northeast-1','non_residential',2199023255552,
                UNIX_TIMESTAMP(),UNIX_TIMESTAMP()+86400*300,0,0,'uat-pn-{int(time.time())}',UNIX_TIMESTAMP(),UNIX_TIMESTAMP());""")
    check(sql("kaitu", f"SELECT COUNT(*) FROM router_fulfillments WHERE user_id={uid};") == "0", "该用户没有任何路由器台账")

    d = create_order(tok, "router-svc-1y")
    check(d.get("code") == 400,
          "定制线路客户不得用 $299 服务套餐续掉 $599 的线路", f"code={d.get('code')} msg={d.get('message')}")
    d = create_order(tok, "router-std-1y",
                     shipping={"name": "李四", "phone": "13900139000", "address": "上海"})
    check(d.get("code") == 0,
          "定制线路客户可以正常购买开途路由器版（不该被『已有路由器』误挡）",
          f"code={d.get('code')} msg={d.get('message')}")


def main() -> None:
    print("=" * 72)
    print("NextPay + 路由器版 端到端 UAT")
    print("=" * 72)
    buyer = f"uat-buyer-{int(time.time())}@kaitu.test"
    token = login(buyer, "uat-device-01")
    uid = sql("kaitu", "SELECT id FROM users ORDER BY id DESC LIMIT 1;")
    # 后台台账/看板需要 ops 角色（RoleDevopsViewer|RoleDevopsEditor），改完要重登让 JWT 带上
    sql("kaitu", f"UPDATE users SET roles = 1|16|32, is_admin=1 WHERE id={uid};")
    token = login(buyer, "uat-device-01")

    paid = scenario_app_purchase(token)
    scenario_durable_link(token, paid)
    scenario_router_gates(token)
    scenario_router_lifecycle(token)
    scenario_admin_and_online(token)
    scenario_renewal(token)
    scenario_cross_product(token)

    print("\n" + "=" * 72)
    if FAILURES:
        print(f"UAT 失败：{len(FAILURES)}/{CHECKS} 项不通过")
        for f in FAILURES:
            print("  ✗ " + f)
        sys.exit(1)
    print(f"UAT 全部通过：{CHECKS}/{CHECKS} 项")


if __name__ == "__main__":
    main()
