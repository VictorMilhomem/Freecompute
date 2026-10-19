#!/usr/bin/env bash
set -euo pipefail

MODE="honest"
if [[ "${1:-}" == "--divergent" ]]; then
    MODE="divergent"
elif [[ "${1:-}" != "" ]]; then
    echo "Usage: $0 [--divergent]" >&2
    exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

missing=0
for bin in freechains ipfs podman ssh-keygen go; do
    if ! command -v "$bin" >/dev/null 2>&1; then
        echo "MISSING prerequisite binary: $bin" >&2
        missing=1
    fi
done

if [[ -z "${VC_IMAGE:-}" ]]; then
    echo "MISSING prerequisite env var: VC_IMAGE (must be repo@sha256:...)" >&2
    missing=1
elif [[ "$VC_IMAGE" != *"@sha256:"* ]]; then
    echo "INVALID VC_IMAGE: must be digest-pinned (repo@sha256:...), got '$VC_IMAGE'" >&2
    missing=1
fi

IPFS_API="${VC_IPFS_API:-http://127.0.0.1:5001}"
if ! curl -fsS -X POST "$IPFS_API/api/v0/id" >/dev/null 2>&1; then
    echo "MISSING running Kubo API at $IPFS_API (start with: ipfs daemon)" >&2
    missing=1
fi

PODMAN_SOCK="${VC_PODMAN_SOCKET:-}"
if [[ -z "$PODMAN_SOCK" ]]; then
    if [[ -n "${XDG_RUNTIME_DIR:-}" ]]; then
        PODMAN_SOCK="unix://${XDG_RUNTIME_DIR}/podman/podman.sock"
    else
        PODMAN_SOCK="unix:///run/user/$(id -u)/podman/podman.sock"
    fi
fi
PODMAN_SOCK_PATH="${PODMAN_SOCK#unix://}"
if [[ ! -S "$PODMAN_SOCK_PATH" ]]; then
    echo "MISSING Podman socket at $PODMAN_SOCK_PATH (start with: podman system service --time=0)" >&2
    missing=1
fi

if [[ "$missing" -ne 0 ]]; then
    exit 1
fi

for stale_pid in /tmp/vc-demo-*/{alice,bob,carol}/{root/daemon.pid,vcnode.pid}; do
    if [[ -f "$stale_pid" ]]; then
        kill -9 "$(cat "$stale_pid")" 2>/dev/null || true
    fi
done
rm -rf /tmp/vc-demo-* 2>/dev/null || true

TMP_DIR="$(mktemp -d -t vc-demo-XXXXXX)"
PIDS=()

REAL_FREECHAINS="$(command -v freechains)"
mkdir -p "$TMP_DIR/bin"
if ! command -v lua5.4 >/dev/null 2>&1 && command -v lua >/dev/null 2>&1; then
    ln -s "$(command -v lua)" "$TMP_DIR/bin/lua5.4"
fi
cat >"$TMP_DIR/bin/freechains" <<EOF
#!/usr/bin/env bash
root=""
is_unlock=0
prev=""
for arg in "\$@"; do
    case "\$arg" in
        --root=*) root="\${arg#--root=}" ;;
        daemon)   is_unlock=1 ;;
        send)     [[ "\$prev" == "sync" ]] && is_unlock=1 ;;
        clone)    [[ "\$prev" == "add" ]] && is_unlock=1 ;;
    esac
    if [[ "\$prev" == "--root" ]]; then
        root="\$arg"
    fi
    prev="\$arg"
done
if [[ -n "\$root" && -d "\$root" && "\$is_unlock" -eq 0 ]] && command -v flock >/dev/null 2>&1; then
    exec flock -w 15 "\$root/.fc.lock" "$REAL_FREECHAINS" "\$@"
else
    exec "$REAL_FREECHAINS" "\$@"
fi
EOF
chmod +x "$TMP_DIR/bin/freechains"
export PATH="$TMP_DIR/bin:$PATH"

cleanup() {
    disown -a 2>/dev/null || true
    for node in alice bob carol; do
        for pf in "$TMP_DIR/$node/vcnode.pid" "$TMP_DIR/$node/root/daemon.pid"; do
            if [[ -f "$pf" ]]; then
                kill -9 "$(cat "$pf")" 2>/dev/null || true
            fi
        done
    done
    for pid in "${PIDS[@]:-}"; do
        if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
            kill -9 "$pid" 2>/dev/null || true
        fi
    done
    rm -rf "$TMP_DIR"
}
trap cleanup EXIT INT TERM

echo "==> Building vcnode..."
VCNODE_BIN="$TMP_DIR/vcnode"
(cd "$REPO_ROOT" && CGO_ENABLED=0 go build -o "$VCNODE_BIN" ./cmd/vcnode)

echo "==> Generating ed25519 keys for alice, bob, carol..."
mkdir -p "$TMP_DIR/keys"
for node in alice bob carol; do
    ssh-keygen -t ed25519 -N "" -C "" -f "$TMP_DIR/keys/$node" -q
done

ALICE_PUB="$(awk '{print $1 " " $2}' "$TMP_DIR/keys/alice.pub")"
BOB_PUB="$(awk '{print $1 " " $2}' "$TMP_DIR/keys/bob.pub")"
CAROL_PUB="$(awk '{print $1 " " $2}' "$TMP_DIR/keys/carol.pub")"

alias_of_pub() {
    local pub="$1"
    case "$pub" in
        "$ALICE_PUB") echo "alice" ;;
        "$BOB_PUB")   echo "bob" ;;
        "$CAROL_PUB") echo "carol" ;;
        *)            echo "$pub" ;;
    esac
}

echo "==> Starting three Freechains daemons (8331, 8332, 8333) with --hub..."
declare -A PORTS=( [alice]=8331 [bob]=8332 [carol]=8333 )
for node in alice bob carol; do
    root="$TMP_DIR/$node/root"
    mkdir -p "$root/chains"
    log="$TMP_DIR/$node/daemon.log"
    freechains --root="$root" daemon start --port="${PORTS[$node]}" --hub -- --reuseaddr >"$log" 2>&1 &
    PIDS+=("$!")
done

for node in alice bob carol; do
    for _ in $(seq 1 50); do
        if [[ -f "$TMP_DIR/$node/root/daemon.pid" ]] && (echo >/dev/tcp/127.0.0.1/"${PORTS[$node]}") >/dev/null 2>&1; then
            break
        fi
        sleep 0.1
    done
done

CHAIN="#vc"
echo "==> Initializing $CHAIN on alice with three pioneers and cloning to bob and carol..."
ALICE_CID="$(freechains --root="$TMP_DIR/alice/root" chains add "$CHAIN" init \
    --pioneer="$TMP_DIR/keys/alice.pub" \
    --pioneer="$TMP_DIR/keys/bob.pub" \
    --pioneer="$TMP_DIR/keys/carol.pub")"
BOB_CID="$(freechains --root="$TMP_DIR/bob/root" chains add "$CHAIN" clone "localhost:${PORTS[alice]}")"
CAROL_CID="$(freechains --root="$TMP_DIR/carol/root" chains add "$CHAIN" clone "localhost:${PORTS[alice]}")"

echo "    alice chain id: $ALICE_CID"
echo "    bob   chain id: $BOB_CID"
echo "    carol chain id: $CAROL_CID"
if [[ "$ALICE_CID" != "$BOB_CID" || "$ALICE_CID" != "$CAROL_CID" ]]; then
    echo "FAIL: chain IDs do not match" >&2
    exit 1
fi

ALICE_REPS_BEFORE="$(freechains --root="$TMP_DIR/alice/root" chain "$CHAIN" reps author "$ALICE_PUB")"
BOB_REPS_BEFORE="$(freechains --root="$TMP_DIR/alice/root" chain "$CHAIN" reps author "$BOB_PUB")"
CAROL_REPS_BEFORE="$(freechains --root="$TMP_DIR/alice/root" chain "$CHAIN" reps author "$CAROL_PUB")"

echo "==> Reputation (before):"
echo "    alice: $ALICE_REPS_BEFORE"
echo "    bob:   $BOB_REPS_BEFORE"
echo "    carol: $CAROL_REPS_BEFORE"

INPUT_FILE="$TMP_DIR/data.txt"
cat >"$INPUT_FILE" <<'EOF'
volunteer computing on freechains ipfs and podman works end to end
EOF

CAROL_BEHAVIOR="honest"
if [[ "$MODE" == "divergent" ]]; then
    CAROL_BEHAVIOR="divergent"
fi

echo "==> Starting worker nodes bob (honest) and carol ($CAROL_BEHAVIOR)..."
"$VCNODE_BIN" run \
    --id=bob \
    --root="$TMP_DIR/bob/root" \
    --key="$TMP_DIR/keys/bob" \
    --chain="$CHAIN" \
    --peers="localhost:8331,localhost:8333" \
    --behavior=honest >"$TMP_DIR/bob/vcnode.log" 2>&1 &
echo "$!" >"$TMP_DIR/bob/vcnode.pid"
PIDS+=("$!")

"$VCNODE_BIN" run \
    --id=carol \
    --root="$TMP_DIR/carol/root" \
    --key="$TMP_DIR/keys/carol" \
    --chain="$CHAIN" \
    --peers="localhost:8331,localhost:8332" \
    --behavior="$CAROL_BEHAVIOR" >"$TMP_DIR/carol/vcnode.log" 2>&1 &
echo "$!" >"$TMP_DIR/carol/vcnode.pid"
PIDS+=("$!")

echo "==> Starting alice with --submit..."
REPORT_FILE="$TMP_DIR/alice_report.txt"
"$VCNODE_BIN" run \
    --id=alice \
    --root="$TMP_DIR/alice/root" \
    --key="$TMP_DIR/keys/alice" \
    --chain="$CHAIN" \
    --peers="localhost:8332,localhost:8333" \
    --behavior=honest \
    --submit="$INPUT_FILE" \
    --grace=20 >"$REPORT_FILE"

echo ""
echo "==> Chain Timeline (consensus order on alice):"
idx=0
while IFS= read -r entry; do
    [[ -z "$entry" ]] && continue
    aid="${entry#\~}"
    aid="${aid%\~}"
    meta="$(freechains --root="$TMP_DIR/alice/root" chain "$CHAIN" get metadata "$aid")"
    kind="$(awk '/^action / {print $2}' <<<"$meta")"
    signer="$(awk '/^sign / {print $2 " " $3}' <<<"$meta")"
    author_alias="$(alias_of_pub "$signer")"
    printf "    [%d] %s  kind=%-6s  author=%s\n" "$idx" "$aid" "$kind" "$author_alias"
    idx=$((idx + 1))
done < <(freechains --root="$TMP_DIR/alice/root" chain "$CHAIN" list order)

echo ""
echo "==> Submitter Report & Verdict:"
cat "$REPORT_FILE"

VERIFIED="$(awk -F= '/^VERIFIED=/ {print $2}' "$REPORT_FILE")"
WINNERS_RAW="$(awk -F= '/^WINNERS=/ {print $2}' "$REPORT_FILE")"
DIVERGENT_RAW="$(awk -F= '/^DIVERGENT=/ {print $2}' "$REPORT_FILE")"

map_csv_aliases() {
    local csv="$1"
    local out=()
    IFS=',' read -ra items <<<"$csv"
    for item in "${items[@]:-}"; do
        [[ -z "$item" ]] && continue
        out+=("$(alias_of_pub "$item")")
    done
    local IFS=','
    echo "${out[*]:-}"
}

WINNERS_ALIASES="$(map_csv_aliases "$WINNERS_RAW")"
DIVERGENT_ALIASES="$(map_csv_aliases "$DIVERGENT_RAW")"
echo "    WINNERS_ALIASES=$WINNERS_ALIASES"
echo "    DIVERGENT_ALIASES=$DIVERGENT_ALIASES"

ALICE_REPS_AFTER="$(freechains --root="$TMP_DIR/alice/root" chain "$CHAIN" reps author "$ALICE_PUB")"
BOB_REPS_AFTER="$(freechains --root="$TMP_DIR/alice/root" chain "$CHAIN" reps author "$BOB_PUB")"
CAROL_REPS_AFTER="$(freechains --root="$TMP_DIR/alice/root" chain "$CHAIN" reps author "$CAROL_PUB")"

echo ""
echo "==> Reputation (after):"
echo "    alice: $ALICE_REPS_AFTER (before: $ALICE_REPS_BEFORE)"
echo "    bob:   $BOB_REPS_AFTER (before: $BOB_REPS_BEFORE)"
echo "    carol: $CAROL_REPS_AFTER (before: $CAROL_REPS_BEFORE)"

pass=1
if [[ "$VERIFIED" != "true" ]]; then
    pass=0
fi

if [[ "$MODE" == "honest" ]]; then
    for w in alice bob carol; do
        if [[ ",$WINNERS_ALIASES," != *",$w,"* ]]; then
            pass=0
        fi
    done
    if [[ -n "$DIVERGENT_ALIASES" ]]; then
        pass=0
    fi
else
    for w in alice bob; do
        if [[ ",$WINNERS_ALIASES," != *",$w,"* ]]; then
            pass=0
        fi
    done
    if [[ "$DIVERGENT_ALIASES" != "carol" ]]; then
        pass=0
    fi
    if [[ "$CAROL_REPS_AFTER" -ge "$CAROL_REPS_BEFORE" ]]; then
        pass=0
    fi
fi

echo ""
if [[ "$pass" -eq 1 ]]; then
    echo "PASS"
    exit 0
else
    echo "FAIL"
    exit 1
fi
