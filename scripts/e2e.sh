#!/usr/bin/env bash
#
# End-to-end verification for art (the automated form of blueprint.md §9.4).
#
# Everything a browser would do is driven through the matching HTTP API with
# curl. Each run builds a fresh vault in a new temporary directory, so the
# script is repeatable; on success the last line is E2E PASS.
#
#   Usage:      make build && scripts/e2e.sh
#   Keep state: ART_E2E_KEEP=1 scripts/e2e.sh
#
# The Markdown and HTML this script writes into the vault stays in Chinese on
# purpose: multi-byte text is exactly where byte-offset anchoring breaks, so
# the fixtures double as UTF-8 coverage.
#
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ART="${ART_BIN:-$ROOT/bin/art}"

if [[ ! -x "$ART" ]]; then
  echo "no executable at ${ART}; run make build first" >&2
  exit 1
fi

WORK="$(mktemp -d "${TMPDIR:-/tmp}/art-e2e.XXXXXX")"
WORK="$(cd "$WORK" && pwd)" # collapse the // a trailing-slash TMPDIR leaves behind, which would break path assertions
VAULT="$WORK/vault"
SERVE_PID=""
SSE_PID=""

cleanup() {
  local code=$?
  [[ -n "$SSE_PID" ]] && kill "$SSE_PID" 2>/dev/null || true
  if [[ -n "$SERVE_PID" ]]; then
    kill "$SERVE_PID" 2>/dev/null || true
    wait "$SERVE_PID" 2>/dev/null || true
  fi
  if [[ "${ART_E2E_KEEP:-}" == "1" ]]; then
    echo "state kept in $WORK"
  else
    rm -rf "$WORK"
  fi
  exit $code
}
trap cleanup EXIT INT TERM

# ---------------------------------------------------------------------------
# Assertions and helpers
# ---------------------------------------------------------------------------

STEP=0
step() { STEP=$((STEP + 1)); printf '\n[%02d] %s\n' "$STEP" "$1"; }
ok()   { printf '     ok  %s\n' "$1"; }
fail() { printf '     FAIL %s\n' "$1" >&2; exit 1; }

# jget <python-expr>: read JSON from stdin into `d`, print the expression's value.
jget() { python3 -c 'import sys,json;d=json.load(sys.stdin);print(eval(sys.argv[1]))' "$1"; }

assert_eq()   { [[ "$1" == "$2" ]] || fail "$3 (want $2, got $1)"; ok "$3"; }
assert_ne()   { [[ "$1" != "$2" ]] || fail "$3 (should not equal $2)"; ok "$3"; }
assert_grep() { grep -q -- "$2" "$1" || fail "$3"; ok "$3"; }

# wait_for <seconds> <command...>: poll until the command succeeds.
wait_for() {
  local budget="$1"; shift
  local i
  for ((i = 0; i < budget * 5; i++)); do
    if "$@" >/dev/null 2>&1; then return 0; fi
    sleep 0.2
  done
  return 1
}

free_port() {
  python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1]);s.close()'
}

art() { "$ART" --vault "$VAULT" "$@"; }

PORT="$(free_port)"
BASE="http://127.0.0.1:$PORT"

start_serve() {
  "$ART" --vault "$VAULT" serve --port "$PORT" >"$WORK/serve.log" 2>&1 &
  SERVE_PID=$!
  wait_for 20 curl -sf "$BASE/api/health" || {
    cat "$WORK/serve.log" >&2
    fail "serve failed to start"
  }
}

stop_serve() {
  [[ -n "$SERVE_PID" ]] || return 0
  kill "$SERVE_PID" 2>/dev/null || true
  wait "$SERVE_PID" 2>/dev/null || true
  SERVE_PID=""
}

echo "art e2e: binary ${ART}, temp vault ${VAULT}, port $PORT"

# ---------------------------------------------------------------------------
step "art init creates a vault"
# ---------------------------------------------------------------------------
mkdir -p "$VAULT"
"$ART" init "$VAULT" >"$WORK/init.log" 2>&1 || { cat "$WORK/init.log" >&2; fail "init failed"; }
[[ -f "$VAULT/.art/config.yaml" ]] || fail "missing .art/config.yaml"
[[ -d "$VAULT/.git" ]] || fail "init did not create a git repository"
ok "vault ready (.art/config.yaml + git)"

# .art/serve.lock holds the --token in plaintext so local CLI calls need no
# configuration. The watcher's auto-commit stages paths, and a user running
# `git add -A` stages everything, so the only thing keeping that token out of
# git history is this ignore rule. Assert the rule exists AND that git really
# honours it, rather than trusting the file's contents.
assert_grep "$VAULT/.gitignore" '^\.art/serve\.lock$' "vault .gitignore ignores .art/serve.lock"
mkdir -p "$VAULT/.art" && : >"$VAULT/.art/serve.lock"
[[ -z "$(git -C "$VAULT" status --porcelain -uall -- .art/serve.lock)" ]] \
  || fail "git still sees .art/serve.lock, so the token could be committed"
rm -f "$VAULT/.art/serve.lock"
ok "git does not track .art/serve.lock (the token cannot leak into history)"

# ---------------------------------------------------------------------------
step "art new creates an md artifact; write multi-block content into it"
# ---------------------------------------------------------------------------
NEW_MD="$(art new payment-refactor --type md --title '支付重构' --json)"
MD_ID="$(printf '%s' "$NEW_MD" | jget 'd["id"]')"
MD_PATH="$(printf '%s' "$NEW_MD" | jget 'd["path"]')"
[[ -n "$MD_ID" && -f "$MD_PATH" ]] || fail "new md produced no file"
ok "md artifact id=$MD_ID path=$MD_PATH"

# The paragraph below is the target; remap and orphan both revolve around it.
TARGET_PARA='这是第一段正文，讲的是支付网关的抽象层设计与幂等键的生成规则。'
cat >>"$MD_PATH" <<EOF

## 背景

$TARGET_PARA

> 引用块：注意重试必须幂等，否则会重复扣款。

- 列表项甲：清理旧的 gateway 分支
- 列表项乙：补 e2e 用例

\`\`\`go
func Pay(ctx context.Context) error { return nil }
\`\`\`

结尾段落，收束全文。
EOF
ok "wrote headings, paragraphs, a blockquote, lists and a code block (Chinese text)"

# ---------------------------------------------------------------------------
step "start serve in the background"
# ---------------------------------------------------------------------------
start_serve
assert_grep "$WORK/serve.log" "watcher started" "serve log confirms the watcher actually started"
assert_grep "$WORK/serve.log" "listening on" "serve printed its listen address"

HEALTH="$(curl -s "$BASE/api/health")"
assert_eq "$(printf '%s' "$HEALTH" | jget 'd["ok"]')" "ok" "GET /api/health returns ok"
assert_eq "$(printf '%s' "$HEALTH" | jget 'str(d["watch"])')" "True" "health reports watch=true"
assert_eq "$(printf '%s' "$HEALTH" | jget 'd["root"]')" "$VAULT" "health root points at this vault"

DOCS="$(curl -s "$BASE/api/docs")"
assert_eq "$(printf '%s' "$DOCS" | jget '[x["id"] for x in d["docs"]].count("'"$MD_ID"'")')" "1" "GET /api/docs lists the new document"

# Embed pipeline: the SPA shell must always render; a binary built via
# make build should additionally serve reviewer.js.
SHELL_HTML="$(curl -s "$BASE/")"
[[ -n "$SHELL_HTML" ]] || fail "GET / returned no SPA shell"
if grep -q 'art-dist-placeholder' <<<"$SHELL_HTML"; then
  ok "SPA shell is the placeholder page (binary built without make build; expected in dev)"
else
  ok "SPA shell is the real frontend build"
  CODE="$(curl -s -o /dev/null -w '%{http_code}' "$BASE/_art/reviewer.js")"
  assert_eq "$CODE" "200" "GET /_art/reviewer.js is served from the embed"
fi

# The watcher's startup ProcessAll auto-commits what we just wrote; that sha
# is the baseline for the historical-version checks below.
wait_for 20 bash -c "git -C '$VAULT' log --oneline -- payment-refactor/index.md | grep -q ." \
  || fail "watcher did not auto-commit the md content"
REV0="$(git -C "$VAULT" rev-parse HEAD)"
ok "recorded history baseline REV0=${REV0:0:8}"

DETAIL="$(curl -s "$BASE/api/docs/$MD_ID")"
assert_grep <(printf '%s' "$DETAIL") 'data-sourcepos' "GET /api/docs/{id} renders HTML carrying data-sourcepos"
assert_eq "$(printf '%s' "$DETAIL" | jget 'd["type"]')" "md" "document type is md"

# ---------------------------------------------------------------------------
step "POST a create event with a selection; expect an exact anchor"
# ---------------------------------------------------------------------------
# Mimic the browser: find the data-sourcepos of the block holding the target
# paragraph, then report the rendered text of the selection inside it.
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
' "$TARGET_PARA"
)
[[ -n "$BLOCK_START" ]] || fail "could not read the block sourcepos"
ok "target block sourcepos=$BLOCK_START:$BLOCK_END"

SEL_EXACT='支付网关的抽象层设计'
CREATE_REQ="$(python3 -c '
import json, sys
print(json.dumps({
  "type": "create",
  "body": "这一段建议拆成两句，幂等键规则单独成段。",
  "selection": {
    "block_start": int(sys.argv[1]),
    "block_end": int(sys.argv[2]),
    "exact": sys.argv[3],
    "before": "这是第一段正文，讲的是",
    "after": "与幂等键的生成规则。",
  },
}, ensure_ascii=False))' "$BLOCK_START" "$BLOCK_END" "$SEL_EXACT")"

CREATE_RES="$(curl -s -X POST "$BASE/api/docs/$MD_ID/events" \
  -H 'Content-Type: application/json' -d "$CREATE_REQ")"
THREAD="$(printf '%s' "$CREATE_RES" | jget 'd["thread"]')"
[[ -n "$THREAD" ]] || fail "create returned no thread id: $CREATE_RES"
ok "created thread $THREAD"

CMTS="$(curl -s "$BASE/api/docs/$MD_ID/comments")"
assert_eq "$(printf '%s' "$CMTS" | jget 'str(d["threads"][0]["anchor"].get("approx", False))')" "False" \
  "anchor hit exactly (approx absent/false)"
assert_eq "$(printf '%s' "$CMTS" | jget 'd["threads"][0]["anchor"]["exact"]')" "$SEL_EXACT" \
  "anchor exact equals the selected text"
A_START="$(printf '%s' "$CMTS" | jget 'd["threads"][0]["anchor"]["start"]')"
A_END="$(printf '%s' "$CMTS" | jget 'd["threads"][0]["anchor"]["end"]')"
ok "anchor offsets [$A_START,$A_END)"

# ---------------------------------------------------------------------------
step "art comments --open --json exposes every field an agent needs"
# ---------------------------------------------------------------------------
art comments --open --json >"$WORK/comments-open.json"
python3 - "$WORK/comments-open.json" "$THREAD" <<'PY' || fail "art comments --open --json is missing fields"
import json, sys
threads = json.load(open(sys.argv[1]))
if isinstance(threads, dict):
    threads = threads.get("threads", [])
t = next(x for x in threads if x["thread"] == sys.argv[2])
missing = [k for k in ("path", "status", "body") if not t.get(k)]
a = t["anchor"]
for k in ("line", "start", "end"):
    if not isinstance(a.get(k), int) or a[k] <= 0:
        missing.append("anchor." + k)
for k in ("exact", "context"):
    if not a.get(k):
        missing.append("anchor." + k)
if missing:
    sys.exit("missing fields: " + ",".join(missing))
print("     ok  path/line/start/end/quote(exact)/context all present, status=%s" % t["status"])
PY

# ---------------------------------------------------------------------------
step "art reply / art addressed route through serve (proven via SSE)"
# ---------------------------------------------------------------------------
# The watcher ignores .art/, so a `comments` SSE event can only come from
# serve's own write path. Receiving one proves the CLI routed through serve
# instead of writing the file directly.
: >"$WORK/sse.log"
curl -sN "$BASE/api/stream" >"$WORK/sse.log" 2>/dev/null &
SSE_PID=$!
sleep 0.6

art reply "$THREAD" "已精简，幂等键规则合并进第 2 节" >/dev/null
art addressed "$THREAD" --note "见 commit" >/dev/null

wait_for 15 grep -q "event: comments" "$WORK/sse.log" \
  || { cat "$WORK/sse.log" >&2; fail "CLI write produced no SSE, so it did not go through serve"; }
ok "received a comments SSE event: CLI-to-API routing confirmed"
[[ -f "$VAULT/.art/serve.lock" ]] || fail "missing .art/serve.lock"
ok "serve.lock present (what the CLI probes for)"
kill "$SSE_PID" 2>/dev/null || true; SSE_PID=""

CMTS="$(curl -s "$BASE/api/docs/$MD_ID/comments")"
assert_eq "$(printf '%s' "$CMTS" | jget 'len(d["threads"][0]["replies"])')" "1" "the reply was persisted"
assert_eq "$(printf '%s' "$CMTS" | jget 'd["threads"][0]["status"]')" "addressed" "status became addressed"

# ---------------------------------------------------------------------------
step "push the commented paragraph down; the watcher must remap offsets correctly"
# ---------------------------------------------------------------------------
INSERT='插入的新段落，用来把后面的内容整体往下推，验证重映射。'
python3 - "$MD_PATH" "$TARGET_PARA" "$INSERT" <<'PY'
import sys
path, target, insert = sys.argv[1], sys.argv[2], sys.argv[3]
src = open(path, encoding="utf-8").read()
i = src.index(target)
open(path, "w", encoding="utf-8").write(src[:i] + insert + "\n\n" + src[i:])
PY
SHIFT="$(python3 -c 'import sys;print(len((sys.argv[1] + "\n\n").encode()))' "$INSERT")"
ok "inserted $SHIFT bytes ahead of the target paragraph"

wait_for 25 bash -c "curl -s '$BASE/api/docs/$MD_ID/comments' | grep -q '\"start\":$((A_START + SHIFT))'" \
  || { curl -s "$BASE/api/docs/$MD_ID/comments" >&2; fail "start did not shift to $((A_START + SHIFT)) after remap"; }
CMTS="$(curl -s "$BASE/api/docs/$MD_ID/comments")"
assert_eq "$(printf '%s' "$CMTS" | jget 'd["threads"][0]["anchor"]["start"]')" "$((A_START + SHIFT))" "start shifted correctly after remap"
assert_eq "$(printf '%s' "$CMTS" | jget 'd["threads"][0]["anchor"]["end"]')" "$((A_END + SHIFT))" "end shifted correctly after remap"
assert_eq "$(printf '%s' "$CMTS" | jget 'd["threads"][0]["anchor"]["exact"]')" "$SEL_EXACT" "exact is unchanged after remap"
assert_grep "$VAULT/.art/comments/$MD_ID.yaml" "e: remap" "a remap event appears in the event log"

# ---------------------------------------------------------------------------
step "delete the commented paragraph; the thread must orphan and keep last_exact"
# ---------------------------------------------------------------------------
python3 - "$MD_PATH" "$TARGET_PARA" <<'PY'
import sys
path, target = sys.argv[1], sys.argv[2]
src = open(path, encoding="utf-8").read()
open(path, "w", encoding="utf-8").write(src.replace(target + "\n\n", "", 1))
PY

wait_for 25 bash -c "curl -s '$BASE/api/docs/$MD_ID/comments' | grep -q '\"orphan\":true'" \
  || { curl -s "$BASE/api/docs/$MD_ID/comments" >&2; fail "thread did not become orphan after deletion"; }
CMTS="$(curl -s "$BASE/api/docs/$MD_ID/comments")"
assert_eq "$(printf '%s' "$CMTS" | jget 'str(d["threads"][0]["anchor"]["orphan"])')" "True" "thread is flagged orphan"
assert_eq "$(printf '%s' "$CMTS" | jget 'd["threads"][0]["anchor"]["last_exact"]')" "$SEL_EXACT" "orphan kept last_exact"
# The orphan hint is a frozen contract duplicated in two places: api.OrphanHint
# on the Go side and ORPHAN_HINT in web/src/lib/types.ts. Nothing but this
# assertion stops the two from drifting apart — and drift is silent, because
# each side stays internally consistent. Compare the value the running server
# actually serves against the constant the frontend actually ships.
TS_HINT="$(python3 - "$ROOT/web/src/lib/types.ts" <<'PY'
import re, sys
src = open(sys.argv[1], encoding="utf-8").read()
m = re.search(r"export const ORPHAN_HINT =\s*'((?:[^'\\]|\\.)*)'", src, re.S)
if not m:
    sys.exit("ORPHAN_HINT not found in web/src/lib/types.ts")
print(m.group(1))
PY
)"
API_HINT="$(printf '%s' "$CMTS" | jget 'd["threads"][0].get("hint", "")')"
assert_ne "$API_HINT" "" "orphan thread carries the fixed hint"
assert_eq "$API_HINT" "$TS_HINT" "api.OrphanHint matches web ORPHAN_HINT byte for byte"
assert_grep "$VAULT/.art/comments/$MD_ID.yaml" "e: orphan" "an orphan event appears in the event log"

# ---------------------------------------------------------------------------
step "curl POST resolve; art comments --all then shows resolved"
# ---------------------------------------------------------------------------
RESOLVE_RES="$(curl -s -X POST "$BASE/api/docs/$MD_ID/events" -H 'Content-Type: application/json' \
  -d "$(python3 -c 'import json,sys;print(json.dumps({"type":"resolve","thread":sys.argv[1]}))' "$THREAD")")"
assert_eq "$(printf '%s' "$RESOLVE_RES" | jget 'd["ok"]')" "ok" "POST resolve succeeded"

art comments --all --json >"$WORK/comments-all.json"
assert_eq "$(jget 'next(x["status"] for x in (d if isinstance(d,list) else d["threads"]) if x["thread"]=="'"$THREAD"'")' <"$WORK/comments-all.json")" \
  "resolved" "art comments --all shows resolved"

# ---------------------------------------------------------------------------
step "art new html; the watcher injects data-aid and restores meta aid"
# ---------------------------------------------------------------------------
NEW_HTML="$(art new demo-page --type html --title '演示页' --json)"
HTML_ID="$(printf '%s' "$NEW_HTML" | jget 'd["id"]')"
HTML_PATH="$(printf '%s' "$NEW_HTML" | jget 'd["path"]')"
ok "html artifact id=$HTML_ID"
git -C "$VAULT" log --oneline -- demo-page/index.html | grep -q . \
  || fail "art new did not commit the skeleton, so the watcher cannot recover the id"
ok "art new committed the aid-bearing skeleton to git"

# From an agent's point of view: overwrite the whole file right after art new,
# with neither a meta aid nor any element data-aid. This is the ordering most
# likely to lose the document id, so the watcher has to recover it from the
# skeleton that was just committed.
cat >"$HTML_PATH" <<'EOF'
<!doctype html>
<html lang="zh-CN">
  <head>
    <meta charset="utf-8" />
    <title>演示页</title>
  </head>
  <body>
    <h1>结算页原型</h1>
    <p>这一段描述结算流程的第一步：确认收货地址。</p>
    <section>
      <p>第二步：选择支付方式并确认金额。</p>
    </section>
  </body>
</html>
EOF

wait_for 25 grep -q 'data-aid' "$HTML_PATH" || { cat "$HTML_PATH" >&2; fail "watcher did not inject data-aid"; }
assert_grep "$HTML_PATH" 'data-aid' "data-aid was injected into the source file"
assert_grep "$HTML_PATH" "content=\"$HTML_ID\"" "meta aid=$HTML_ID was recovered (overwritten before the first commit)"

RAW="$(curl -s "$BASE/raw/$HTML_ID/")"
assert_grep <(printf '%s' "$RAW") '/_art/reviewer.js' "GET /raw/{id}/ has the reviewer script injected"

AID="$(printf '%s' "$RAW" | python3 -c '
import sys, re
m = re.search(r"data-aid=\"([0-9a-z]+)\"", sys.stdin.read())
print(m.group(1) if m else "")')"
[[ -n "$AID" ]] || fail "could not read a data-aid"
EL_RES="$(curl -s -X POST "$BASE/api/docs/$HTML_ID/events" -H 'Content-Type: application/json' \
  -d "$(python3 -c 'import json,sys;print(json.dumps({"type":"create","body":"这一步要加一个地址校验的说明。","element":{"aid":sys.argv[1],"quote":"结算页原型"}},ensure_ascii=False))' "$AID")")"
EL_THREAD="$(printf '%s' "$EL_RES" | jget 'd["thread"]')"
[[ -n "$EL_THREAD" ]] || fail "creating the element-anchored comment failed: $EL_RES"
EL_CMTS="$(curl -s "$BASE/api/docs/$HTML_ID/comments")"
assert_eq "$(printf '%s' "$EL_CMTS" | jget 'd["threads"][0]["anchor"]["kind"]')" "element" "the html thread uses an element anchor"
assert_eq "$(printf '%s' "$EL_CMTS" | jget 'd["threads"][0]["anchor"]["aid"]')" "$AID" "the element anchor points at the injected aid"

# ---------------------------------------------------------------------------
step "overwrite an html doc that already has comments; id recovers, comments stay attached"
# ---------------------------------------------------------------------------
# The previous step covered "overwritten right after creation". This one
# covers "overwritten after content and comments accumulated": once recovered,
# the existing threads must still hang off the same document.
cat >"$HTML_PATH" <<'EOF'
<!doctype html>
<html lang="zh-CN">
  <head>
    <meta charset="utf-8" />
    <title>演示页</title>
  </head>
  <body>
    <h1>结算页原型（改版）</h1>
    <p>整体重写后的正文，meta aid 被 agent 冲掉了。</p>
  </body>
</html>
EOF
wait_for 25 grep -q "content=\"$HTML_ID\"" "$HTML_PATH" \
  || { cat "$HTML_PATH" >&2; fail "watcher did not restore the meta aid"; }
assert_grep "$HTML_PATH" "content=\"$HTML_ID\"" "meta aid was restored to the original id=$HTML_ID"
assert_eq "$(curl -s "$BASE/api/docs/$HTML_ID/comments" | jget 'len(d["threads"])')" "1" \
  "existing comments still belong to this document after recovery"

# ---------------------------------------------------------------------------
step "historical version ?v=<sha>: renders old content, writes return 409"
# ---------------------------------------------------------------------------
HIST="$(curl -s "$BASE/api/docs/$MD_ID?v=$REV0")"
assert_eq "$(printf '%s' "$HIST" | jget 'd["rev0"]')" "$REV0" "the ?v=sha response echoes rev0"
printf '%s' "$HIST" | jget 'd["html"]' | grep -q "$TARGET_PARA" \
  || fail "the historical version did not render the deleted paragraph"
ok "historical version renders the old content, including the deleted paragraph"
CUR="$(curl -s "$BASE/api/docs/$MD_ID")"
printf '%s' "$CUR" | jget 'd["html"]' | grep -q "$TARGET_PARA" \
  && fail "the current version still contains the deleted paragraph" || ok "current version no longer has it, so the two really differ"

CODE="$(curl -s -o "$WORK/hist-post.json" -w '%{http_code}' -X POST "$BASE/api/docs/$MD_ID/events?v=$REV0" \
  -H 'Content-Type: application/json' \
  -d "$(python3 -c 'import json,sys;print(json.dumps({"type":"reply","thread":sys.argv[1],"body":"x"}))' "$THREAD")")"
assert_eq "$CODE" "409" "POST events against a historical version returns 409"
assert_eq "$(jget 'd["error"]' <"$WORK/hist-post.json")" "conflict" "the 409 error code is conflict"

# ---------------------------------------------------------------------------
step "security: path traversal"
# ---------------------------------------------------------------------------
CODE="$(curl -s --path-as-is -o /dev/null -w '%{http_code}' "$BASE/raw/$HTML_ID/../../etc/passwd")"
assert_eq "$CODE" "404" "GET /raw/{id}/../../etc/passwd returns 404"

# ---------------------------------------------------------------------------
step "art compact --force collapses edit/remap chains and archives aged resolved threads"
# ---------------------------------------------------------------------------
art compact --force --json >"$WORK/compact.json" 2>&1 || { cat "$WORK/compact.json" >&2; fail "compact failed"; }
assert_eq "$(jget 'str(any(s["events_after"] < s["events_before"] for s in d["stats"]))' <"$WORK/compact.json")" \
  "True" "compact collapsed the event chain"

# Archiving only takes threads that are resolved AND older than ResolvedAge
# (30 days); --force skips the size threshold only (blueprint §4.6). Backdate
# every timestamp in the log by 40 days so the archive path is reachable.
python3 - "$VAULT/.art/comments/$MD_ID.yaml" <<'PY'
import re, sys
from datetime import datetime, timedelta
path = sys.argv[1]
src = open(path, encoding="utf-8").read()
def back(m):
    ts = datetime.fromisoformat(m.group(1))
    return "ts: " + (ts - timedelta(days=40)).isoformat()
open(path, "w", encoding="utf-8").write(re.sub(r"ts: (\S+)", back, src))
PY
art compact --force --json >"$WORK/compact2.json" 2>&1 || { cat "$WORK/compact2.json" >&2; fail "the second compact failed"; }
wait_for 10 test -f "$VAULT/.art/comments/$MD_ID.archive.yaml" \
  || { cat "$WORK/compact2.json" >&2; fail "no archive file $MD_ID.archive.yaml was produced"; }
assert_grep "$VAULT/.art/comments/$MD_ID.archive.yaml" "e: archive" "the archive file holds archive events"
assert_eq "$(jget 'str(sum(s["threads_archived"] for s in d["stats"]))' <"$WORK/compact2.json")" "1" \
  "exactly 1 resolved thread was archived"
ok "archive file .art/comments/$MD_ID.archive.yaml created"

# ---------------------------------------------------------------------------
step "risk 2: API output with serve matches direct CLI reads without serve, field for field"
# ---------------------------------------------------------------------------
curl -s "$BASE/api/docs/$HTML_ID/comments" | jget 'json.dumps(d["threads"],sort_keys=True,ensure_ascii=False)' \
  >"$WORK/threads-api.json"
stop_serve
art comments --doc "$HTML_ID" --all --json \
  | jget 'json.dumps((d if isinstance(d,list) else d["threads"]),sort_keys=True,ensure_ascii=False)' \
  >"$WORK/threads-cli.json"
diff -u "$WORK/threads-api.json" "$WORK/threads-cli.json" >"$WORK/threads.diff" 2>&1 \
  || { cat "$WORK/threads.diff" >&2; fail "the threads arrays from the CLI and the HTTP API differ"; }
ok "art comments --json and GET /api/docs/{id}/comments agree field for field"

# ---------------------------------------------------------------------------
step "the direct-write CLI path still works without serve"
# ---------------------------------------------------------------------------
art reply "$EL_THREAD" "无 serve 直写的回复" >/dev/null
art comments --doc "$HTML_ID" --all --json \
  | jget 'str(any(r["body"]=="无 serve 直写的回复" for t in (d if isinstance(d,list) else d["threads"]) for r in t["replies"]))' \
  | grep -qx True || fail "art reply did not persist without serve"
ok "art reply writes directly when serve is down"

# ---------------------------------------------------------------------------
step "art doctor: exit 0 on a clean vault, non-zero when issues remain"
# ---------------------------------------------------------------------------
art doctor >"$WORK/doctor.log" 2>&1 || { cat "$WORK/doctor.log" >&2; fail "art doctor exited non-zero"; }
ok "art doctor exits 0 on a clean vault: $(head -1 "$WORK/doctor.log")"

# The exit code has to be directly consumable by agents and CI: drop in an
# artifact with no aid, and doctor must report missing-aid and exit non-zero;
# remove it again and doctor must go back to 0.
mkdir -p "$VAULT/stray"
printf '# 没有 frontmatter aid 的文档\n' >"$VAULT/stray/index.md"
set +e
art doctor >"$WORK/doctor-dirty.log" 2>&1
RC=$?
set -e
[[ $RC -ne 0 ]] || { cat "$WORK/doctor-dirty.log" >&2; fail "art doctor still exited 0 with an unresolved issue"; }
assert_grep "$WORK/doctor-dirty.log" "missing-aid" "doctor reports missing-aid"
ok "art doctor exits ${RC} (non-zero) with an unresolved issue"
rm -rf "$VAULT/stray"
art doctor >/dev/null 2>&1 || fail "doctor should return to exit 0 once the bad artifact is gone"
ok "art doctor is back to exit 0 after the issue is removed"

# ---------------------------------------------------------------------------
step "security: --host 0.0.0.0 without a token must refuse to start"
# ---------------------------------------------------------------------------
set +e
"$ART" --vault "$VAULT" serve --host 0.0.0.0 --port "$PORT" >"$WORK/host-notoken.log" 2>&1
RC=$?
set -e
[[ $RC -ne 0 ]] || { cat "$WORK/host-notoken.log" >&2; fail "--host 0.0.0.0 started without a token"; }
ok "--host 0.0.0.0 refused to start without a token (exit ${RC})"

# ---------------------------------------------------------------------------
step "security: authentication under --host 0.0.0.0 --token"
# ---------------------------------------------------------------------------
TOKEN="e2e-secret-$RANDOM"
PORT="$(free_port)"
BASE="http://127.0.0.1:$PORT"
"$ART" --vault "$VAULT" serve --host 0.0.0.0 --port "$PORT" --token "$TOKEN" >"$WORK/serve-token.log" 2>&1 &
SERVE_PID=$!
wait_for 20 bash -c "curl -sf -H 'Authorization: Bearer $TOKEN' '$BASE/api/health' >/dev/null" \
  || { cat "$WORK/serve-token.log" >&2; fail "serve with a token failed to start"; }

CODE="$(curl -s -o /dev/null -w '%{http_code}' "$BASE/api/docs")"
assert_eq "$CODE" "401" "a request without a token returns 401"
CODE="$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $TOKEN" "$BASE/api/docs")"
assert_eq "$CODE" "200" "a request with a Bearer token returns 200"
CODE="$(curl -s -o /dev/null -w '%{http_code}' "$BASE/api/docs?token=$TOKEN")"
assert_eq "$CODE" "200" "the ?token= query parameter is accepted too"
stop_serve

printf '\nE2E PASS\n'
