# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this project does

`trae-proxy` is a single Go binary that intercepts HTTPS traffic destined for `openrouter.ai` and reroutes it to any Anthropic Messages API-compatible upstream. It handles three request types:

1. **Anthropic Messages passthrough** (`POST /v1/messages`) — strips `/api` prefix, maps model names, streams response back unchanged
2. **Chat Completions translation** (`POST /v1/chat/completions`) — converts OpenAI-format requests to Anthropic Messages format, forwards upstream, converts response back; supports streaming (SSE) and tool use
3. **Fake models endpoint** (`GET /v1/models`) — returns the model list from config so clients (e.g. Trae) can validate model IDs

DNS hijack via `/etc/hosts` makes `openrouter.ai` resolve to `127.0.0.1`. trae-proxy terminates TLS on :443 using a self-signed cert it generates itself.

> **Warning**: While the proxy is active, `openrouter.ai` resolves to localhost — real OpenRouter is unreachable from any local app or browser.

## Architecture

```
Client  (ANTHROPIC_BASE_URL=https://openrouter.ai/api)
      │
      ↓  [/etc/hosts: openrouter.ai → 127.0.0.1]
trae-proxy :443  (built-in TLS, self-signed cert)
      │
      ├─ GET  /v1/models           → fake model list (no upstream call)
      ├─ POST /v1/chat/completions → convert to Anthropic → upstream → convert back
      └─ POST /v1/messages + other → strip /api prefix + map model → passthrough
      │
      ↓
Upstream Anthropic Messages API (e.g. sub2api at 192.168.48.12:8080)
```

## Commands

```bash
sudo trae-proxy init       # generate cert, install CA to system trust store (one-time)
sudo trae-proxy start      # add hosts entry, start proxy
sudo trae-proxy start -d   # start as background daemon
sudo trae-proxy stop       # stop daemon, remove hosts entry
trae-proxy status          # show running state
```

Build from source:

```bash
make build       # build for current platform → bin/trae-proxy
make build-all   # cross-compile all platforms
make install     # build + sudo cp to /usr/local/bin
```

## Key configuration

Config file: `~/.config/trae-proxy/config.toml`

- **Upstream**: configurable, default `http://192.168.48.12:8080`
- **Listen**: `:443`
- **Hijack domain**: `openrouter.ai`
- **Model mapping**: three-level fallback — exact match → strip `anthropic/` prefix → passthrough

## Project structure

```
cmd/trae-proxy/main.go           # CLI entry (cobra)
internal/
  config/config.go               # TOML config, model mapping
  proxy/
    server.go                    # HTTPS server, route dispatch
    handler.go                   # Chat Completions HTTP handler
    chat.go                      # request/response format conversion
    chat_stream.go               # SSE stream conversion state machine
    convert.go                   # message/content/tool conversion
    forward.go                   # generic passthrough proxy
    models.go                    # fake models list endpoint
    util.go                      # UUID generation
  tls/ca.go                      # CA generation, cert signing, system trust
  hosts/hosts.go                 # /etc/hosts management (cross-platform)
  daemon/                        # daemon mode (Unix/Windows)
```

## Git remotes and branch rules

This project has two remotes with **separate branches**:

| Remote | URL | Branch |
|--------|-----|--------|
| `origin` | GitLab (internal) | `master` |
| `github` | `git@github.com:DASungta/tare-proxy.git` | `main` |

**Branch strategy:**

- `master` → GitLab only (`git push origin master`)
- `main` → GitHub only (`git push github main`)
- `main` branch does **not** contain `CLAUDE.md` (removed via `git rm --cached`)

**Sync workflow** (after committing to `master`):

```bash
git checkout main
git merge master          # CLAUDE.md stays absent on main (merge respects the deletion)
git push github main
git checkout master
```

**Rules:**
- `CLAUDE.md` lives only on `master` → GitLab. Never push it to GitHub.
- `main` is a public-facing branch; keep it clean of internal tooling files.
- Go module path stays `github.com/zhangyc/trae-proxy` (internal import path, unrelated to the GitHub repo name `DASungta/tare-proxy`).
