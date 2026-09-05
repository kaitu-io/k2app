#!/usr/bin/env bash
# 幂等创建 Overleap Stripe 订阅资源（Product + 年/月 Price）。
#
# 币种规则（2026-09-04，spec 2026-09-04-overleap-site-decoupling-and-uk-positioning-design.md §4）：
#   主币 USD —— Stripe 账号是 US 主体、结算币 USD；只有主币 = 结算币时 Adaptive Pricing 才会
#   把其余国家换算成本币。currency_options 里 GBP / EUR 是固定本币价（Checkout 按客户属地自动选）。
#   本脚本的三组数字是网站定价表的唯一上游：web/tests/pricing-source.test.ts 解析下面的
#   `ensure_price` 行与 web/src/lib/site/overleap.ts 的 pricing 表逐币种对比，改价必须同步。
#
# 用法：STRIPE_SECRET_KEY=sk_... scripts/stripe-setup-overleap.sh
# test / live key 均可。幂等：Price 按 lookup_key 找；既有且主币已是 usd → 只输出 id；
# 既有但主币不是 usd（2026-07 建的 EUR 主币价）→ 归档旧价，再以 transfer_lookup_key 新建。
# Product 按 metadata.slug 找。脚本本身零密钥，绝不 echo key。
set -euo pipefail

: "${STRIPE_SECRET_KEY:?set STRIPE_SECRET_KEY in env (never commit or echo it)}"
API=https://api.stripe.com/v1

req() { curl -sS -u "${STRIPE_SECRET_KEY}:" "$@"; }
obj_id() { python3 -c 'import json,sys; o=json.load(sys.stdin); print(o.get("id") or sys.exit("stripe error: %s" % o))'; }
first_id_by_slug() { python3 -c 'import json,sys; d=json.load(sys.stdin).get("data",[]); ids=[o["id"] for o in d if o.get("metadata",{}).get("slug")==sys.argv[1]]; print(ids[0] if ids else "")' "$1"; }
# 输出 "<id> <currency>"，无则空串
first_id_currency() { python3 -c 'import json,sys; d=json.load(sys.stdin).get("data",[]); print("%s %s" % (d[0]["id"], d[0]["currency"]) if d else "")'; }

# --- Product（metadata.slug 幂等；list+本地过滤，避免 search 端点最终一致性导致重复创建）---
product_id=$(req "$API/products" -G -d "active=true" -d "limit=100" | first_id_by_slug overleap-basic)
if [ -z "$product_id" ]; then
  product_id=$(req "$API/products" \
    -d name="Overleap Basic" \
    -d "metadata[slug]=overleap-basic" | obj_id)
  echo "created product: $product_id"
else
  echo "product exists:  $product_id"
fi
# 账单描述符挂在 Product 上，而不是改账号级设置：Stripe 账号是 Wordgate LLC 共用账号，账号级
# 描述符（ARBELLA）与对外名称要保持中性；Product.statement_descriptor 会整体覆盖该产品订阅扣款的
# calculated_statement_descriptor（2026-09-05 test 模式实测：1199 usd 扣款 → "OVERLEAP"）。
# 每次都 POST，幂等；≤22 字符、仅字母数字与空格。
req "$API/products/$product_id" -d statement_descriptor=OVERLEAP | obj_id >/dev/null
echo "product statement_descriptor: OVERLEAP"

ensure_price() { # $1=lookup_key $2=interval $3=usd $4=gbp $5=eur（单位：最小货币单位）
  local existing existing_id existing_currency
  existing=$(req "$API/prices" -G -d "lookup_keys[]=$1" -d active=true | first_id_currency)
  existing_id=${existing%% *}
  existing_currency=${existing#* }
  if [ -n "$existing_id" ] && [ "$existing_currency" = "usd" ]; then
    echo "$1 exists:  $existing_id"
    return
  fi
  if [ -n "$existing_id" ]; then
    # Price 币种不可改：归档旧主币价（既有订阅不受影响），lookup_key 随新价转移。
    req "$API/prices/$existing_id" -d active=false >/dev/null
    echo "$1 archived $existing_currency price: $existing_id"
  fi
  local id
  id=$(req "$API/prices" \
    -d product="$product_id" \
    -d currency=usd \
    -d unit_amount="$3" \
    -d "recurring[interval]=$2" \
    -d lookup_key="$1" \
    -d transfer_lookup_key=true \
    -d "currency_options[gbp][unit_amount]=$4" \
    -d "currency_options[eur][unit_amount]=$5" | obj_id)
  echo "created $1: $id"
}

#             lookup_key        interval usd  gbp  eur
ensure_price  overleap_basic_1y year     7900 7900 8900
ensure_price  overleap_basic_1m month    1199  999 1199

echo "done. 把上面两个 price id 填进 Plan 行（stripe_price_id；Plan.price 为 USD 分：7900 / 1199）。"
