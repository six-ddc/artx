#!/usr/bin/env bash
#
# Browser smoke: start a real serve, build the exact scenario that used to
# crash the document page into the router's error boundary — a Markdown doc
# with nested headings and lists plus one thread that has no replies — then
# open it in a real Chromium. The lib unit tests, tsc and vite build never
# mount the component tree; only this step does.
#
#   Usage:      make build && scripts/smoke.sh
#   Keep state: ART_SMOKE_KEEP=1 scripts/smoke.sh
#
# The Markdown written into the vault stays in Chinese on purpose: multi-byte
# text is where byte-offset anchoring breaks, so the fixture doubles as UTF-8
# coverage.
#
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ART="${ART_BIN:-$ROOT/bin/art}"

if [[ ! -x "$ART" ]]; then
  echo "no executable at ${ART}; run make build first" >&2
  exit 1
fi
if [[ ! -f "$ROOT/web/scripts/browser-smoke.mjs" ]]; then
  echo "web/scripts/browser-smoke.mjs not found" >&2
  exit 1
fi

WORK="$(mktemp -d "${TMPDIR:-/tmp}/art-smoke.XXXXXX")"
WORK="$(cd "$WORK" && pwd)"
VAULT="$WORK/vault"
SERVE_PID=""

cleanup() {
  local code=$?
  if [[ -n "$SERVE_PID" ]]; then
    kill "$SERVE_PID" 2>/dev/null || true
    wait "$SERVE_PID" 2>/dev/null || true
  fi
  if [[ "${ART_SMOKE_KEEP:-}" == "1" ]]; then
    echo "state kept in $WORK"
  else
    rm -rf "$WORK"
  fi
  exit $code
}
trap cleanup EXIT INT TERM

fail() { printf '     FAIL %s\n' "$1" >&2; exit 1; }
jget() { python3 -c 'import sys,json;d=json.load(sys.stdin);print(eval(sys.argv[1]))' "$1"; }

wait_for() {
  local budget="$1"; shift
  local i
  for ((i = 0; i < budget * 5; i++)); do
    if "$@" >/dev/null 2>&1; then return 0; fi
    sleep 0.2
  done
  return 1
}

PORT="$(python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1]);s.close()')"
BASE="http://127.0.0.1:$PORT"

echo "art smoke: binary ${ART}, temp vault ${VAULT}, port $PORT"

mkdir -p "$VAULT"
"$ART" init "$VAULT" >"$WORK/init.log" 2>&1 || { cat "$WORK/init.log" >&2; fail "init failed"; }

NEW="$("$ART" --vault "$VAULT" new checkout-flow --type md --title '结算流程' --json)"
DOC="$(printf '%s' "$NEW" | jget 'd["id"]')"
MD="$(printf '%s' "$NEW" | jget 'd["path"]')"

TARGET='第一步是确认收货地址，这里要区分默认地址与本次下单临时改的地址。'
cat >>"$MD" <<EOF

## 一级：主流程

$TARGET

### 二级：异常分支

- 地址库为空时走新建地址表单
- 地址超出配送范围时提示换地址
- 收货人手机号缺失时阻断下一步

#### 三级：埋点

1. 进入结算页
2. 选中地址
3. 提交订单

> 备注：三级标题下的有序列表是之前崩页的组合。
EOF

"$ART" --vault "$VAULT" serve --port "$PORT" >"$WORK/serve.log" 2>&1 &
SERVE_PID=$!
wait_for 20 curl -sf "$BASE/api/health" || { cat "$WORK/serve.log" >&2; fail "serve failed to start"; }

DETAIL="$(curl -s "$BASE/api/docs/$DOC")"
read -r BLOCK_START BLOCK_END < <(
  printf '%s' "$DETAIL" | python3 -c '
import sys, json, re
d = json.load(sys.stdin)
target = sys.argv[1]
for m in re.finditer(r"<p data-sourcepos=\"(\d+):(\d+)\">(.*?)</p>", d["html"], re.S):
    if target in m.group(3):
        print(m.group(1), m.group(2)); break
else:
    sys.exit("no data-sourcepos block matches the target paragraph")
' "$TARGET"
)
[[ -n "$BLOCK_START" ]] || fail "could not read the block sourcepos"

# The point of this thread is that it has zero replies. The moment the
# backend serializes replies as null, ThreadCard blows up on replies.map and
# the whole page falls into the router's error boundary.
REQ="$(python3 -c '
import json, sys
print(json.dumps({
  "type": "create",
  "body": "这一段建议把默认地址与临时地址拆成两句。",
  "selection": {
    "block_start": int(sys.argv[1]),
    "block_end": int(sys.argv[2]),
    "exact": "确认收货地址",
    "before": "第一步是",
    "after": "，这里要区分",
  },
}, ensure_ascii=False))' "$BLOCK_START" "$BLOCK_END")"
RES="$(curl -s -X POST "$BASE/api/docs/$DOC/events" -H 'Content-Type: application/json' -d "$REQ")"
THREAD="$(printf '%s' "$RES" | jget 'd["thread"]')"
[[ -n "$THREAD" ]] || fail "creating the comment thread failed: $RES"

REPLIES="$(curl -s "$BASE/api/docs/$DOC/comments" | jget 'json.dumps(d["threads"][0]["replies"])')"
[[ "$REPLIES" == "[]" ]] || fail "a reply-less thread must serialize replies as [], got $REPLIES"
echo "scenario ready: doc ${DOC}, reply-less thread ${THREAD}, replies=[]"

cd "$ROOT/web"
ART_SMOKE_URL="$BASE" node scripts/browser-smoke.mjs
