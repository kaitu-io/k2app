#!/usr/bin/env bash
# 幂等创建 Overleap Stripe 订阅资源（Product + 年/月 Price，EUR 主币种 + USD/GBP currency_options）。
# 用法：STRIPE_SECRET_KEY=sk_... scripts/stripe-setup-overleap.sh
# test / live key 均可；已存在（lookup_key / metadata.slug 命中）则只输出既有 id。
# 定价决策见 docs/superpowers/specs/2026-07-22-overleap-stripe-web-design.md §1。
set -euo pipefail

: "${STRIPE_SECRET_KEY:?set STRIPE_SECRET_KEY in env (never commit or echo it)}"
API=https://api.stripe.com/v1

req() { curl -sS -u "${STRIPE_SECRET_KEY}:" "$@"; }
first_id() { python3 -c 'import json,sys; d=json.load(sys.stdin).get("data",[]); print(d[0]["id"] if d else "")'; }
obj_id() { python3 -c 'import json,sys; o=json.load(sys.stdin); print(o.get("id") or sys.exit("stripe error: %s" % o))'; }
first_id_by_slug() { python3 -c 'import json,sys; d=json.load(sys.stdin).get("data",[]); ids=[o["id"] for o in d if o.get("metadata",{}).get("slug")==sys.argv[1]]; print(ids[0] if ids else "")' "$1"; }

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

ensure_price() { # $1=lookup_key $2=interval $3=eur $4=usd $5=gbp（单位：分）
  local existing
  existing=$(req "$API/prices" -G -d "lookup_keys[]=$1" -d active=true | first_id)
  if [ -n "$existing" ]; then
    echo "$1 exists:  $existing"
    return
  fi
  local id
  id=$(req "$API/prices" \
    -d product="$product_id" \
    -d currency=eur \
    -d unit_amount="$3" \
    -d "recurring[interval]=$2" \
    -d lookup_key="$1" \
    -d "currency_options[usd][unit_amount]=$4" \
    -d "currency_options[gbp][unit_amount]=$5" | obj_id)
  echo "created $1: $id"
}

ensure_price overleap_basic_1y year  8900 7900 7900
ensure_price overleap_basic_1m month 1199 1199  999

echo "done. 把上面两个 price id 填进 Plan 行（stripe_price_id）。"
