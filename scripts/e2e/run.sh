#!/usr/bin/env bash
# End-to-end test for the live-routes plugin against a locally built CLIProxyAPI and a mock upstream.
# Never touches ~/.cli-proxy-api: CPA runs with HOME and cwd inside a scratch dir. No real accounts, no network.
#
# Usage: scripts/e2e/run.sh [--dry-run] [--only-infra] [--keep]
#   --dry-run     build CPA, check the mock, generate the config, print it, start nothing
#   --only-infra  build, start mock + CPA (plugin disabled), send one request through CPA, stop
#   --keep        keep the scratch directory
# Env: CPA_SRC (default ../CLIProxyAPI next to this repo), CPA_BIN (prebuilt binary, skips the build)
set -u

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CPA_SRC="${CPA_SRC:-$ROOT/../CLIProxyAPI}"
MODE=full
KEEP=0
for a in "$@"; do
  case "$a" in
    --dry-run) MODE=dry ;;
    --only-infra) MODE=infra ;;
    --keep) KEEP=1 ;;
    *) echo "unknown argument: $a" >&2; exit 2 ;;
  esac
done

GOOS_H="$(go env GOOS)"
GOARCH_H="$(go env GOARCH)"
case "$GOOS_H" in darwin) EXT=dylib ;; windows) EXT=dll ;; *) EXT=so ;; esac

WORK="$(mktemp -d "${TMPDIR:-/tmp}/lr-e2e.XXXXXX")"
KEY1="sk-e2e-test-key-ONE-aaaa1111"
KEY2="sk-e2e-test-key-TWO-bbbb2222"
MGMT="e2e-mgmt-secret-9f3a"
UPSTREAM_KEY="mock-upstream-key"
BOGUS_VAL=bogus
PASS=0
FAIL=0
FAILED_NAMES=()
MOCK_PID=""
CPA_PID=""
PORT=""
MOCK_PORT=""

cleanup() {
  stop_cpa
  [ -n "$MOCK_PID" ] && kill "$MOCK_PID" 2>/dev/null
  if [ "$KEEP" = 1 ]; then echo "scratch kept: $WORK"; else rm -rf "$WORK"; fi
}
trap cleanup EXIT
trap 'exit 130' INT TERM

ok() { PASS=$((PASS + 1)); echo "  PASS  $1"; }
bad() { FAIL=$((FAIL + 1)); FAILED_NAMES+=("$1"); echo "  FAIL  $1${2:+ -- $2}"; }
check() { # check "name" command...
  local name="$1"; shift
  if "$@" >/dev/null 2>&1; then ok "$name"; else bad "$name"; fi
}
step() { echo; echo "== $*"; }

free_port() { python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1])'; }

wait_for() { # wait_for <seconds> <cmd...>
  local n=$(($1 * 10)); shift
  while [ "$n" -gt 0 ]; do
    if "$@" >/dev/null 2>&1; then return 0; fi
    sleep 0.1; n=$((n - 1))
  done
  return 1
}

# ---------------------------------------------------------------------------- build
step "build CPA core and mock upstream"
if [ -n "${CPA_BIN:-}" ]; then
  cp "$CPA_BIN" "$WORK/cpa"
else
  [ -d "$CPA_SRC/cmd/server" ] || { echo "CPA sources not found at $CPA_SRC (set CPA_SRC)"; exit 2; }
  if go -C "$CPA_SRC" build -o "$WORK/cpa" ./cmd/server 2>"$WORK/cpa-build.log"; then
    ok "CPA core built"
  else
    bad "CPA core build"; tail -20 "$WORK/cpa-build.log"; exit 1
  fi
fi
node --check "$ROOT/scripts/e2e/mock-upstream.mjs" && ok "mock upstream syntax" || bad "mock upstream syntax"

PLUGIN_LIB=""
step "build plugin"
if make -C "$ROOT" build >"$WORK/plugin-build.log" 2>&1 && [ -f "$ROOT/bin/live-routes.$EXT" ]; then
  mkdir -p "$WORK/plugins/$GOOS_H/$GOARCH_H"
  cp "$ROOT/bin/live-routes.$EXT" "$WORK/plugins/$GOOS_H/$GOARCH_H/"
  PLUGIN_LIB="$WORK/plugins/$GOOS_H/$GOARCH_H/live-routes.$EXT"
  ok "plugin built and installed"
else
  tail -15 "$WORK/plugin-build.log"
  if [ "$MODE" = full ]; then bad "plugin build"; exit 1; fi
  echo "  (plugin build failed; continuing because mode=$MODE)"
fi

# ---------------------------------------------------------------------------- config
# write_config <plugin_enabled> <inflight> <require_key>
write_config() {
  mkdir -p "$WORK/run" "$WORK/home" "$WORK/auth" "$WORK/plugins"
  cat >"$WORK/config.yaml" <<EOF
config-version: 8
server:
  host: "127.0.0.1"
  port: $PORT
management:
  allow-remote: false
  secret-key: "${MGMT}"
  disable-control-panel: true
access:
  api-keys:
    - "$KEY1"
    - "$KEY2"
oauth:
  auth-dir: "$WORK/auth"
routing:
  retry:
    request-retry: 0
api-keys:
  openai-compatibility:
    - name: "mock"
      base-url: "http://127.0.0.1:$MOCK_PORT/v1"
      disable-cooling: true
      request-retry: 0
      keys:
        - api-key: "${UPSTREAM_KEY}"
      models:
        - name: "mock-slow"
          alias: "opus"
        - name: "mock-fast"
          alias: "haiku"
        - name: "mock-429"
          alias: "limited"
plugins:
  enabled: $1
  dir: "$WORK/plugins"
  configs:
    live-routes:
      enabled: $1
      priority: -100
      inflight_tracking: $2
      require_key: $3
EOF
}

start_mock() {
  MOCK_PORT="$(free_port)"
  PORT="$MOCK_PORT" node "$ROOT/scripts/e2e/mock-upstream.mjs" >"$WORK/mock.log" 2>&1 &
  MOCK_PID=$!
  wait_for 10 curl -sf "http://127.0.0.1:$MOCK_PORT/healthz" && ok "mock upstream up on $MOCK_PORT" || { bad "mock upstream start"; exit 1; }
}

stop_cpa() {
  if [ -n "$CPA_PID" ]; then
    kill "$CPA_PID" 2>/dev/null
    local i=0
    while kill -0 "$CPA_PID" 2>/dev/null && [ $i -lt 50 ]; do sleep 0.1; i=$((i + 1)); done
    kill -9 "$CPA_PID" 2>/dev/null
    wait "$CPA_PID" 2>/dev/null
    CPA_PID=""
  fi
}

# start_cpa <plugin_enabled> <inflight> <require_key>
start_cpa() {
  stop_cpa
  PORT="$(free_port)"
  write_config "$1" "$2" "$3"
  ( cd "$WORK/run" && HOME="$WORK/home" exec "$WORK/cpa" -config "$WORK/config.yaml" -local-model ) >"$WORK/cpa.log" 2>&1 &
  CPA_PID=$!
  if ! wait_for 40 curl -s -o /dev/null -H "Authorization: Bearer ${KEY1}" "http://127.0.0.1:$PORT/v1/models"; then
    bad "CPA start (plugin=$1 inflight=$2 require_key=$3)"; tail -30 "$WORK/cpa.log"; exit 1
  fi
  ok "CPA up on $PORT (plugin=$1 inflight=$2 require_key=$3)"
}

BASE() { echo "http://127.0.0.1:$PORT"; }
RES() { echo "$(BASE)/v0/resource/plugins/live-routes"; }
# CPA does not forward the Host header to plugins; the plugin derives the host from Origin/Referer like a browser page does.
REF_LOCAL() { echo "Referer: http://127.0.0.1:$PORT/v0/resource/plugins/live-routes/live"; }
REF_REMOTE() { echo "Referer: http://192.168.1.2:$PORT/v0/resource/plugins/live-routes/live"; }

uuid() { python3 -c 'import uuid;print(uuid.uuid4())'; }

# claude_req <out> <model> <key> <session> [agent-id] ; Anthropic-style streaming request
claude_req() {
  local out="$1" model="$2" key="$3" sid="$4" agent="${5:-}"
  local extra=()
  [ -n "$agent" ] && extra=(-H "X-Claude-Code-Agent-Id: $agent")
  curl -s -N -m 60 -o "$out" -w '%{http_code}' -X POST "$(BASE)/v1/messages" \
    -H "x-api-key: ${key}" -H "anthropic-version: 2023-06-01" -H 'content-type: application/json' \
    -H 'User-Agent: claude-cli/2.0.14 (external, cli)' -H "X-Claude-Code-Session-Id: $sid" ${extra[@]+"${extra[@]}"} \
    -d "{\"model\":\"$model\",\"max_tokens\":64,\"stream\":true,\"messages\":[{\"role\":\"user\",\"content\":\"hello\"}]}"
}

# codex_req <out> <model> <key> <session>
codex_req() {
  curl -s -N -m 60 -o "$1" -w '%{http_code}' -X POST "$(BASE)/v1/chat/completions" \
    -H "Authorization: Bearer ${3}" -H 'content-type: application/json' \
    -H 'User-Agent: codex_cli_rs/0.46.0' -H "Session-Id: $4" \
    -d "{\"model\":\"$2\",\"stream\":true,\"messages\":[{\"role\":\"user\",\"content\":\"hello\"}]}"
}

# ---------------------------------------------------------------------------- dry run
step "start mock upstream"
start_mock

if [ "$MODE" = dry ]; then
  PORT=18317
  write_config true true false
  step "generated config (dry run)"
  cat "$WORK/config.yaml"
  echo; echo "dry run done: CPA built, mock checked, config generated, nothing started"
  echo "RESULT: PASS=$PASS FAIL=$FAIL"
  [ "$FAIL" = 0 ]; exit $?
fi

if [ "$MODE" = infra ]; then
  step "infra smoke test (plugin disabled)"
  start_cpa false true false
  code=$(curl -s -m 20 -o "$WORK/infra.json" -w '%{http_code}' -X POST "$(BASE)/v1/chat/completions" \
    -H "Authorization: Bearer ${KEY1}" -H 'content-type: application/json' \
    -d '{"model":"haiku","messages":[{"role":"user","content":"hi"}]}')
  [ "$code" = 200 ] && ok "chat via CPA to mock (200)" || { bad "chat via CPA to mock" "HTTP $code: $(head -c 300 "$WORK/infra.json")"; tail -20 "$WORK/cpa.log"; }
  code=$(curl -s -m 20 -o "$WORK/infra429.json" -w '%{http_code}' -X POST "$(BASE)/v1/chat/completions" \
    -H "Authorization: Bearer ${KEY1}" -H 'content-type: application/json' \
    -d '{"model":"limited","messages":[{"role":"user","content":"hi"}]}')
  [ "$code" = 429 ] && ok "limited alias yields 429" || bad "limited alias yields 429" "HTTP $code"
  code=$(claude_req "$WORK/infra_claude.out" haiku "$KEY1" "$(uuid)")
  [ "$code" = 200 ] && ok "Anthropic-style streaming request via CPA (200)" || bad "Anthropic-style streaming request via CPA" "HTTP $code: $(head -c 300 "$WORK/infra_claude.out")"
  code=$(codex_req "$WORK/infra_codex.out" haiku "$KEY2" "$(uuid)")
  [ "$code" = 200 ] && ok "OpenAI-style streaming request via CPA (200)" || bad "OpenAI-style streaming request via CPA" "HTTP $code"
  echo; echo "RESULT: PASS=$PASS FAIL=$FAIL"
  [ "$FAIL" = 0 ]; exit $?
fi

# ---------------------------------------------------------------------------- phase A
step "A. plugin enabled, require_key=false, inflight_tracking=true"
start_cpa true true false

check "plugin snapshot route answers on loopback" curl -sf -H "$(REF_LOCAL)" "$(RES)/api/snapshot"
check "plugin page route answers" curl -sf "$(RES)/live"

S_CLAUDE="$(uuid)"
S_CODEX="$(uuid)"
claude_req "$WORK/r_slow.out" opus "$KEY1" "$S_CLAUDE" >"$WORK/r_slow.code" &
P_SLOW=$!
sleep 0.3
claude_req "$WORK/r_agent.out" haiku "$KEY1" "$S_CLAUDE" explore-1 >"$WORK/r_agent.code" &
P_AGENT=$!
codex_req "$WORK/r_codex.out" haiku "$KEY2" "$S_CODEX" >"$WORK/r_codex.code" &
P_CODEX=$!
codex_req "$WORK/r_429.out" limited "$KEY2" "$S_CODEX" >"$WORK/r_429.code" &
P_429=$!

sleep 2.5
curl -s -H "$(REF_LOCAL)" "$(RES)/api/snapshot" >"$WORK/snap_mid.json"
python3 - "$WORK/snap_mid.json" <<'PY' && ok "in-flight record visible (queued or routed) while slow stream runs" || bad "in-flight record visible while slow stream runs" "$(head -c 400 "$WORK/snap_mid.json")"
import json, sys
s = json.load(open(sys.argv[1]))
states = [r["state"] for r in s["records"]]
assert any(x in ("queued", "routed") for x in states), states
PY

wait $P_SLOW $P_AGENT $P_CODEX $P_429
[ "$(cat "$WORK/r_slow.code")" = 200 ] && ok "slow stream via CPA returned 200" || bad "slow stream via CPA" "HTTP $(cat "$WORK/r_slow.code")"
[ "$(cat "$WORK/r_agent.code")" = 200 ] && ok "sub-agent request returned 200" || bad "sub-agent request" "HTTP $(cat "$WORK/r_agent.code")"
[ "$(cat "$WORK/r_codex.code")" = 200 ] && ok "codex-like request returned 200" || bad "codex-like request" "HTTP $(cat "$WORK/r_codex.code")"
[ "$(cat "$WORK/r_429.code")" = 429 ] && ok "limited alias returned 429" || bad "limited alias" "HTTP $(cat "$WORK/r_429.code")"
sleep 0.5

curl -s -H "$(REF_LOCAL)" "$(RES)/api/snapshot" >"$WORK/snap_end.json"
python3 - "$WORK/snap_end.json" <<'PY' && ok "done records carry token usage" || bad "done records carry token usage"
import json, sys
s = json.load(open(sys.argv[1]))
done = [r for r in s["records"] if r["state"] == "done"]
assert done, [r["state"] for r in s["records"]]
assert any(r.get("tok") and r["tok"]["total"] > 0 for r in done), done
PY
python3 - "$WORK/snap_end.json" <<'PY' && ok "failed 429 record with retry_after" || bad "failed 429 record with retry_after"
import json, sys
s = json.load(open(sys.argv[1]))
f = [r for r in s["records"] if r["state"] == "failed"]
assert f, [r["state"] for r in s["records"]]
assert any(r.get("retry_after", 0) > 0 for r in f), f
PY
python3 - "$WORK/snap_end.json" <<'PY' && ok "sub-agent session has a parent link" || bad "sub-agent session has a parent link"
import json, sys
s = json.load(open(sys.argv[1]))
sess = s["dict"].get("sessions", [])
assert any(x.get("parent") for x in sess), sess
PY
python3 - "$WORK/snap_end.json" <<'PY' && ok "two distinct clients (claude-code and codex)" || bad "two distinct clients"
import json, sys
s = json.load(open(sys.argv[1]))
kinds = {c["kind"] for c in s["dict"].get("clients", [])}
assert "claude-code" in kinds and "codex" in kinds, kinds
PY

CURSOR="$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["cursor"])' "$WORK/snap_mid.json")"
curl -s -H "$(REF_LOCAL)" "$(RES)/api/events?since=$CURSOR" >"$WORK/events.json"
python3 - "$WORK/events.json" <<'PY' && ok "events since mid-run cursor return changes" || bad "events since mid-run cursor return changes" "$(head -c 300 "$WORK/events.json")"
import json, sys
e = json.load(open(sys.argv[1]))
assert not e.get("reset"), "reset"
assert len(e["events"]) > 0
PY
EC="$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["cursor"])' "$WORK/events.json")"
curl -s -H "$(REF_LOCAL)" "$(RES)/api/events?since=$EC" >"$WORK/events2.json"
python3 -c 'import json,sys;e=json.load(open(sys.argv[1]));assert not e.get("reset") and e["events"]==[]' "$WORK/events2.json" \
  && ok "events at head cursor are empty" || bad "events at head cursor are empty"

LEAK=0
for f in snap_mid.json snap_end.json events.json events2.json; do
  for k in "$KEY1" "$KEY2" "$MGMT" "${UPSTREAM_KEY}"; do
    grep -q -F "$k" "$WORK/$f" && LEAK=1 && echo "    leaked $k in $f"
  done
done
[ "$LEAK" = 0 ] && ok "raw API keys never appear in snapshot/events" || bad "raw API keys never appear in snapshot/events"

code=$(curl -s -o "$WORK/forbidden.json" -w '%{http_code}' -H "$(REF_REMOTE)" "$(RES)/api/snapshot")
{ [ "$code" = 403 ] && grep -q forbidden_host "$WORK/forbidden.json"; } && ok "non-loopback Referer gets 403 forbidden_host" || bad "non-loopback Referer gets 403 forbidden_host" "HTTP $code $(head -c 200 "$WORK/forbidden.json")"
code=$(curl -s -o "$WORK/forbidden2.json" -w '%{http_code}' "$(RES)/api/snapshot")
{ [ "$code" = 403 ] && grep -q forbidden_host "$WORK/forbidden2.json"; } && ok "no Origin/Referer gets 403 forbidden_host" || bad "no Origin/Referer gets 403 forbidden_host" "HTTP $code $(head -c 200 "$WORK/forbidden2.json")"

# runtime file inventory (plugin enabled)
lsof_regular() { # lsof_regular <pid> -> sorted regular file paths
  lsof -p "$1" -Fftn 2>/dev/null | python3 -c '
import sys
t = None
out = set()
for line in sys.stdin:
    line = line.rstrip("\n")
    if line.startswith("f"): t = None
    elif line.startswith("t"): t = line[1:]
    elif line.startswith("n") and t == "REG" and line[1:].startswith("/"): out.add(line[1:].replace("//", "/").replace("/private/var/", "/var/", 1))
print("\n".join(sorted(out)))'
}
lsof_regular "$CPA_PID" >"$WORK/files_enabled.txt"

# ---------------------------------------------------------------------------- phase B
step "B. require_key=true"
start_cpa true true true
code=$(curl -s -o "$WORK/b401.json" -w '%{http_code}' "$(RES)/api/snapshot")
{ [ "$code" = 401 ] && grep -q unauthorized "$WORK/b401.json"; } && ok "snapshot without token -> 401 unauthorized" || bad "snapshot without token -> 401" "HTTP $code"
code=$(curl -s -o /dev/null -w '%{http_code}' -H "X-Live-Routes-Token: ${BOGUS_VAL}" "$(RES)/api/snapshot")
[ "$code" = 401 ] && ok "bogus token -> 401" || bad "bogus token -> 401" "HTTP $code"
code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$(BASE)/v0/management/live-routes/session")
{ [ "$code" = 401 ] || [ "$code" = 403 ]; } && ok "session without management key refused ($code)" || bad "session without management key refused" "HTTP $code"
curl -s -X POST -H "Authorization: Bearer ${MGMT}" "$(BASE)/v0/management/live-routes/session" >"$WORK/session.json"
VIEW_TOK="$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["token"])' "$WORK/session.json" 2>/dev/null)"
[ -n "${VIEW_TOK}" ] && ok "management key exchanged for a view token" || bad "management key exchanged for a view token" "$(head -c 300 "$WORK/session.json")"
code=$(curl -s -o "$WORK/b200.json" -w '%{http_code}' -H "X-Live-Routes-Token: ${VIEW_TOK}" "$(RES)/api/snapshot")
[ "$code" = 200 ] && ok "snapshot with token -> 200" || bad "snapshot with token -> 200" "HTTP $code"
code=$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer ${MGMT}" "$(BASE)/v0/management/live-routes/snapshot")
[ "$code" = 200 ] && ok "management snapshot with key -> 200" || bad "management snapshot with key -> 200" "HTTP $code"
grep -q -F "$MGMT" "$WORK/session.json" "$WORK/b200.json" && bad "management key not echoed" || ok "management key not echoed"

# ---------------------------------------------------------------------------- phase C
step "C. runtime files: plugin disabled vs enabled"
start_cpa false true false
curl -s -o /dev/null -X POST "$(BASE)/v1/chat/completions" -H "Authorization: Bearer ${KEY1}" -H 'content-type: application/json' \
  -d '{"model":"haiku","messages":[{"role":"user","content":"hi"}]}'
lsof_regular "$CPA_PID" >"$WORK/files_disabled.txt"
comm -13 "$WORK/files_disabled.txt" "$WORK/files_enabled.txt" >"$WORK/files_added.txt"
# Ignore the plugin library itself and OS-managed read-only locations (dyld caches, frameworks, locale data).
WORK_N="$(printf %s "$WORK" | sed -e 's|//*|/|g' -e 's|^/private/var/|/var/|')"
grep -v -F -x -e "$WORK_N/config.yaml" "$WORK/files_added.txt" | grep -v -E "live-routes\.(dylib|so|dll)$|^/System/|^/usr/lib/|^/usr/share/|^/Library/|^/var/db/|^/private/etc/localtime|^/dev/" >"$WORK/files_unexpected.txt" || true
if [ -s "$WORK/files_unexpected.txt" ]; then
  bad "plugin adds no regular files beyond its library" "see below"; sed 's/^/    /' "$WORK/files_unexpected.txt"
else
  ok "plugin adds no regular files beyond its library ($(wc -l <"$WORK/files_added.txt" | tr -d ' ') ignored/own entries)"
fi
ls -R "$WORK/plugins" | grep -v '^$' | sed 's/^/    /'
grep -q -E "live-routes\.$EXT" "$WORK/files_enabled.txt" && ok "plugin library was mapped when enabled" || echo "  note: library not listed by lsof (may be reported as a mapping type other than REG)"

# ---------------------------------------------------------------------------- phase D
step "D. 1 MB request body latency, inflight_tracking true vs false"
python3 - "$WORK/big.json" <<'PY'
import json, sys
json.dump({"model": "haiku", "messages": [{"role": "user", "content": "x" * (1 << 20)}]}, open(sys.argv[1], "w"))
PY
measure() { # measure <label> <inflight>
  start_cpa true "$2" false
  for _ in 1 2 3; do
    curl -s -o /dev/null -X POST "$(BASE)/v1/chat/completions" -H "Authorization: Bearer ${KEY1}" -H 'content-type: application/json' --data-binary @"$WORK/big.json"
  done
  : >"$WORK/lat_$1.txt"
  for _ in $(seq 1 30); do
    curl -s -o /dev/null -w '%{time_total}\n' -X POST "$(BASE)/v1/chat/completions" -H "Authorization: Bearer ${KEY1}" \
      -H 'content-type: application/json' --data-binary @"$WORK/big.json" >>"$WORK/lat_$1.txt"
  done
}
measure on true
measure off false
python3 - "$WORK/lat_on.txt" "$WORK/lat_off.txt" <<'PY'
import sys
def load(p):
    v = sorted(float(x) * 1000 for x in open(p).read().split())
    return v
def pct(v, q):
    return v[min(len(v) - 1, int(round(q * (len(v) - 1))))]
on, off = load(sys.argv[1]), load(sys.argv[2])
for name, v in (("inflight_tracking=true ", on), ("inflight_tracking=false", off)):
    print("    %s n=%d p50=%.2f ms p99=%.2f ms" % (name, len(v), pct(v, .5), pct(v, .99)))
print("    delta (true - false): p50=%+.2f ms p99=%+.2f ms" % (pct(on, .5) - pct(off, .5), pct(on, .99) - pct(off, .99)))
PY
ok "latency measured (informational, not asserted)"

# ---------------------------------------------------------------------------- summary
echo
echo "================ SUMMARY ================"
echo "PASS=$PASS FAIL=$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  failed: %s\n' "${FAILED_NAMES[@]}"
  echo "RESULT: FAIL"
  exit 1
fi
echo "RESULT: PASS"
