# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with this repository.

## What this project does

`trae-proxy` intercepts HTTPS traffic destined for `openrouter.ai` and reroutes it to a local `sub2api` instance that speaks the Anthropic Messages API. It solves two problems:

1. **DNS hijack**: `proxy.sh` adds `127.0.0.1 openrouter.ai` to `/etc/hosts` so any tool configured with `ANTHROPIC_BASE_URL=https://openrouter.ai/api` hits localhost instead.
2. **Passthrough + rewrite**: `translator.py` strips the `/api` path prefix (added by the Anthropic SDK) and maps OpenRouter-style model names to sub2api model names. Body structure is otherwise untouched.

Caddy sits in the middle as a TLS terminator: it receives HTTPS on port 443 (with a locally-trusted cert) and forwards plain HTTP to `translator.py` on port 18080.

> **Warning**: While the proxy is active, `openrouter.ai` resolves to localhost on this machine — real OpenRouter is unreachable from any local app or browser.

## Architecture

```
Claude Code / Trae  (Anthropic SDK, ANTHROPIC_BASE_URL=https://openrouter.ai/api)
      │  HTTPS POST /api/v1/messages
      ↓  [/etc/hosts: openrouter.ai → 127.0.0.1]
Caddy :443  (TLS termination, tls internal, host=openrouter.ai)
      ↓  [plain HTTP]
translator.py :18080
      ↓  strip /api prefix + map model name
sub2api  192.168.48.12:8080  (Anthropic Messages API)
```

**Trae / Claude Code config** (`env` block):
```json
{
  "ANTHROPIC_BASE_URL": "https://openrouter.ai/api",
  "ANTHROPIC_MODEL": "anthropic/claude-sonnet-4.6",
  "ANTHROPIC_DEFAULT_HAIKU_MODEL": "anthropic/claude-haiku-4.5",
  "ANTHROPIC_DEFAULT_SONNET_MODEL": "anthropic/claude-sonnet-4.6",
  "ANTHROPIC_DEFAULT_OPUS_MODEL": "anthropic/claude-opus-4.6"
}
```

## Commands

```bash
# Start the full proxy stack (adds hosts entry, starts translator + Caddy, trusts cert)
./proxy.sh start

# Stop everything and remove the hosts entry
./proxy.sh stop

# Check running state
./proxy.sh status

# Run translator directly (for debugging)
python3 translator.py

# Tail logs
tail -f translator.log
```

`proxy.sh start` requires `sudo` (for `/etc/hosts` and `caddy trust`). Caddy must be installed (`brew install caddy`).

## Key configuration

- **Upstream**: `http://192.168.48.12:8080` (hardcoded in `translator.py` as `UPSTREAM`)
- **Translator listen address**: `127.0.0.1:18080`
- **Model mapping** (`MODEL_MAP` in `translator.py`): maps OpenRouter-style names to sub2api model names
  - `anthropic/claude-haiku-4.5` → `claude-haiku-4-5-20251001`
  - `anthropic/claude-sonnet-4.6` → `claude-sonnet-4-6`
  - `anthropic/claude-opus-4.6` → `claude-opus-4-6`
- **Path rewrite**: incoming `/api/v1/messages` → upstream `/v1/messages` (strips the `/api` prefix that the Anthropic SDK appends to the base URL)

## translator.py internals

- `_rewrite_body()` — parses JSON body, maps `model` field via `MODEL_MAP`, re-encodes; skips non-JSON bodies silently
- `_forward()` — strips `/api` prefix, rewrites body, forwards all headers in `FORWARD_HEADERS` (including `anthropic-version`, `anthropic-beta`), streams the upstream response back in 4 KB chunks without buffering; handles both plain JSON and SSE (`text/event-stream`) transparently

## Runtime files

- `.caddy.pid` / `.translator.pid` — PID files created by `proxy.sh`, gitignored
- `translator.log` — stdout/stderr of the translator process
