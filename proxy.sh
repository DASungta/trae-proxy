#!/usr/bin/env bash
set -euo pipefail

HOSTS_ENTRY="127.0.0.1 openrouter.ai"
HOSTS_MARKER="# trae-proxy"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CADDY_PID_FILE="$SCRIPT_DIR/.caddy.pid"
TRANSLATOR_PID_FILE="$SCRIPT_DIR/.translator.pid"

# ── helpers ──────────────────────────────────────────────────────────────────

check_caddy() {
    if ! command -v caddy &>/dev/null; then
        echo "[error] caddy not found. Install with: brew install caddy"
        exit 1
    fi
}

hosts_has_entry() {
    grep -qF "$HOSTS_ENTRY" /etc/hosts 2>/dev/null
}

add_hosts() {
    if hosts_has_entry; then
        echo "[hosts] entry already present"
        return
    fi
    echo "[hosts] adding: $HOSTS_ENTRY"
    echo "$HOSTS_ENTRY $HOSTS_MARKER" | sudo tee -a /etc/hosts >/dev/null
    sudo dscacheutil -flushcache
    sudo killall -HUP mDNSResponder 2>/dev/null || true
}

remove_hosts() {
    if ! hosts_has_entry; then
        echo "[hosts] entry not found, nothing to remove"
        return
    fi
    echo "[hosts] removing: $HOSTS_ENTRY"
    sudo sed -i '' "/$HOSTS_MARKER/d" /etc/hosts
    sudo dscacheutil -flushcache
    sudo killall -HUP mDNSResponder 2>/dev/null || true
}

caddy_running() {
    if [[ -f "$CADDY_PID_FILE" ]]; then
        local pid
        pid=$(cat "$CADDY_PID_FILE")
        kill -0 "$pid" 2>/dev/null
    else
        return 1
    fi
}

# ── commands ─────────────────────────────────────────────────────────────────

cmd_start() {
    check_caddy
    add_hosts

    if caddy_running; then
        echo "[caddy] already running (pid $(cat "$CADDY_PID_FILE"))"
        return
    fi

    echo "[translator] starting..."
    python3 "$SCRIPT_DIR/translator.py" >> "$SCRIPT_DIR/translator.log" 2>&1 &
    echo $! > "$TRANSLATOR_PID_FILE"
    sleep 1
    echo "[translator] started (pid $(cat "$TRANSLATOR_PID_FILE"))"

    echo "[caddy] starting..."
    cd "$SCRIPT_DIR"
    caddy start --config "$SCRIPT_DIR/Caddyfile" --pidfile "$CADDY_PID_FILE"
    echo "[caddy] started (pid $(cat "$CADDY_PID_FILE"))"

    echo "[caddy] trusting local CA (may prompt for password)..."
    sleep 1
    sudo caddy trust

    echo ""
    echo "Proxy is active. Set ANTHROPIC_BASE_URL=https://openrouter.ai/api in your Claude Code / Trae config."
    echo "NOTE: openrouter.ai is redirected to localhost — real OpenRouter is unreachable while proxy is active."
}

cmd_stop() {
    if caddy_running; then
        echo "[caddy] stopping..."
        cd "$SCRIPT_DIR"
        caddy stop
        rm -f "$CADDY_PID_FILE"
        echo "[caddy] stopped"
    else
        echo "[caddy] not running"
    fi

    if [[ -f "$TRANSLATOR_PID_FILE" ]]; then
        local pid
        pid=$(cat "$TRANSLATOR_PID_FILE")
        if kill -0 "$pid" 2>/dev/null; then
            echo "[translator] stopping (pid $pid)..."
            kill "$pid"
        fi
        rm -f "$TRANSLATOR_PID_FILE"
        echo "[translator] stopped"
    else
        echo "[translator] not running"
    fi

    remove_hosts
    echo ""
    echo "Proxy deactivated."
}

cmd_status() {
    echo "=== trae-proxy status ==="
    echo ""

    if hosts_has_entry; then
        echo "[hosts] ✓ api.deepseek.com → 127.0.0.1"
    else
        echo "[hosts] ✗ api.deepseek.com not redirected"
    fi

    if caddy_running; then
        echo "[caddy] ✓ running (pid $(cat "$CADDY_PID_FILE"))"
    else
        echo "[caddy] ✗ not running"
    fi

    if [[ -f "$TRANSLATOR_PID_FILE" ]]; then
        local pid
        pid=$(cat "$TRANSLATOR_PID_FILE")
        if kill -0 "$pid" 2>/dev/null; then
            echo "[translator] ✓ running (pid $pid)"
        else
            echo "[translator] ✗ dead (stale pid)"
        fi
    else
        echo "[translator] ✗ not running"
    fi

    echo ""
    echo "Upstream: http://192.168.48.12:8080"
    echo "Models: anthropic/claude-haiku-4.5→claude-haiku-4-5-20251001 | anthropic/claude-sonnet-4.6→claude-sonnet-4-6 | anthropic/claude-opus-4.6→claude-opus-4-6"
    echo "NOTE: openrouter.ai is redirected to localhost — real OpenRouter is unreachable while proxy is active"
}

# ── main ─────────────────────────────────────────────────────────────────────

case "${1:-}" in
    start)  cmd_start ;;
    stop)   cmd_stop ;;
    status) cmd_status ;;
    *)
        echo "Usage: $0 {start|stop|status}"
        echo ""
        echo "  start   — add hosts entry, trust cert, start Caddy"
        echo "  stop    — stop Caddy, remove hosts entry"
        echo "  status  — show current state"
        exit 1
        ;;
esac
