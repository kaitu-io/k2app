#!/usr/bin/env bash
# 把 docs/customer-service/<brand>/NN-*.md 全量同步到该品牌的 OpenAI vector store（客服 AI 的知识库）。
#
# 用法:
#   OPENAI_API_KEY=sk-... scripts/sync-kb.sh <brand> <vector_store_id>   # 同步到已有 store
#   OPENAI_API_KEY=sk-... scripts/sync-kb.sh <brand> --create            # 新建 store 再同步，打印 id
#   加 --dry-run 只列出将要上传的文件，不调用 API。
#
# 品牌 → 线上配置 openai.filesearch.stores.<BrandConfig.ChatAIStore>.vector_store_id（api/brand.go）：
#   kaitu → stores.crm，overleap → stores.overleap。--create 之后要把打印的 id 写进两台 center 的 config.yml。
#
# 顺序：先上传并挂上新文件、确认全部 completed，再摘掉旧文件——中途失败时 store 里旧内容仍在，
# 客服 AI 不会面对一个空知识库。
set -euo pipefail

BRAND="${1:-}"; TARGET="${2:-}"; DRY="${3:-}"
API="https://api.openai.com/v1"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DIR="$ROOT/docs/customer-service/$BRAND"

die() { echo "error: $*" >&2; exit 1; }
[[ -n "$BRAND" && -n "$TARGET" ]] || die "usage: $0 <kaitu|overleap> <vector_store_id|--create> [--dry-run]"
[[ -d "$DIR" ]] || die "no such KB dir: $DIR"
shopt -s nullglob
FILES=("$DIR"/[0-9][0-9]-*.md)
(( ${#FILES[@]} > 0 )) || die "no NN-*.md files in $DIR"

echo "brand=$BRAND files=${#FILES[@]}"
for f in "${FILES[@]}"; do echo "  $(basename "$f")"; done
[[ "$DRY" == "--dry-run" || "$TARGET" == "--dry-run" ]] && { echo "(dry run)"; exit 0; }

[[ -n "${OPENAI_API_KEY:-}" ]] || die "OPENAI_API_KEY is not set"
AUTH=(-H "Authorization: Bearer $OPENAI_API_KEY")
api() { # api METHOD PATH [curl args...] → body；HTTP 非 2xx 直接失败
  local m="$1" p="$2"; shift 2
  local out code
  out=$(curl -sS -X "$m" "${AUTH[@]}" -w $'\n%{http_code}' "$API$p" "$@")
  code="${out##*$'\n'}"; out="${out%$'\n'*}"
  [[ "$code" == 2* ]] || die "$m $p → HTTP $code: $out"
  printf '%s' "$out"
}

if [[ "$TARGET" == "--create" ]]; then
  STORE=$(api POST /vector_stores -H "Content-Type: application/json" \
    -d "$(jq -n --arg n "customer-service-$BRAND" '{name:$n}')" | jq -r .id)
  echo "created vector store: $STORE"
else
  STORE="$TARGET"
  api GET "/vector_stores/$STORE" >/dev/null
fi

# 旧文件清单（分页取全）
OLD=(); after=""
while :; do
  page=$(api GET "/vector_stores/$STORE/files?limit=100${after:+&after=$after}")
  while read -r id; do [[ -n "$id" ]] && OLD+=("$id"); done < <(jq -r '.data[].id' <<<"$page")
  [[ $(jq -r .has_more <<<"$page") == "true" ]] || break
  after=$(jq -r .last_id <<<"$page")
done
echo "existing files in store: ${#OLD[@]}"

NEW=()
for f in "${FILES[@]}"; do
  fid=$(api POST /files -F purpose=assistants -F "file=@$f;type=text/markdown" | jq -r .id)
  api POST "/vector_stores/$STORE/files" -H "Content-Type: application/json" \
    -d "$(jq -n --arg id "$fid" '{file_id:$id}')" >/dev/null
  NEW+=("$fid"); echo "  uploaded $(basename "$f") → $fid"
done

echo "waiting for indexing..."
for fid in "${NEW[@]}"; do
  for _ in $(seq 1 60); do
    st=$(api GET "/vector_stores/$STORE/files/$fid" | jq -r .status)
    [[ "$st" == "completed" ]] && break
    [[ "$st" == "in_progress" ]] || die "file $fid indexing status=$st (old files kept)"
    sleep 2
  done
  [[ "$st" == "completed" ]] || die "file $fid still $st after 120s (old files kept)"
done

for fid in "${OLD[@]}"; do
  api DELETE "/vector_stores/$STORE/files/$fid" >/dev/null
  # 底层文件删不掉不影响知识库内容（已从 store 摘掉），只是占存储，不中断
  curl -sS -o /dev/null -X DELETE "${AUTH[@]}" "$API/files/$fid" || true
done
echo "removed ${#OLD[@]} old files"
echo "done: store $STORE now has ${#NEW[@]} files for brand $BRAND"
