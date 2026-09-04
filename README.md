# openrouter-free-sync

A [CLIProxyAPI (CPA)](https://github.com/router-for-me/CLIProxyAPI) plugin that automatically syncs free OpenRouter models into your CPA `openai-compatibility` provider config.

## Features

- **Auto-sync on a timer** — configurable interval (default: 24h)
- **Manual refresh** — one-click from the web panel
- **Configurable filters** — all via web UI:
  - Minimum context length (default: 512K)
  - Pricing filter (`free` = $0 input + $0 output, or `any`)
  - Excluded providers (default: `openai/,anthropic/,google/` for China region)
  - Require text output modality
  - Require tools/function-calling support
- **Web panel** — view synced models, edit config, trigger refresh
- **Hot-reload** — config changes take effect immediately, no CPA restart

## How It Works

```
┌─────────────────────────────────────────────────┐
│ openrouter-free-sync plugin (inside CPA process) │
│                                                   │
│  ┌──────────┐   host.http.do   ┌──────────────┐ │
│  │  Ticker  │─────────────────▶│ OpenRouter   │ │
│  │ (goroutine)│  GET /models   │ API          │ │
│  └──────────┘                  └──────────────┘ │
│       │                                 │         │
│       │ filter (ctx, price, provider)   │         │
│       ▼                                 ▼         │
│  ┌──────────┐   host.http.do   ┌──────────────┐ │
│  │ Syncer   │─────────────────▶│ CPA mgmt API │ │
│  │          │  PATCH /v0/mgmt  │ openai-compat │ │
│  └──────────┘                  └──────────────┘ │
│       │                                 │         │
│       ▼                                 ▼         │
│  ┌──────────────────────────────────────────────┐│
│  │ /v1/models shows updated free models ✅     ││
│  └──────────────────────────────────────────────┘│
└─────────────────────────────────────────────────┘
```

The plugin uses CPA's `management_api` capability and `host.http.do` callback. It does **not** implement its own executor or translator — the existing OpenAI-compatibility layer handles request routing.

## Installation

### From source

```bash
git clone https://github.com/wenrouXN/openrouter-free-sync.git
cd openrouter-free-sync
make build
```

This produces `openrouter-free-sync.so` (Linux). Copy it to your CPA plugins directory:

```bash
cp openrouter-free-sync.so /path/to/cliproxyapi/plugins/
```

### CPA plugin store

Add this registry URL to your CPA plugin store sources:

```
https://raw.githubusercontent.com/wenrouXN/openrouter-free-sync/main/registry.json
```

Then install via the store UI.

## Configuration

Add to your CPA `config.yaml`:

```yaml
plugins:
  enabled: true
  dir: "plugins"
  configs:
    openrouter-free-sync:
      enabled: true
      priority: 1
      refresh_interval: "24h"           # auto-sync interval
      min_context_length: 512000        # minimum context length
      pricing_filter: "free"            # free | any
      excluded_providers:               # provider prefixes to exclude
        - "openai/"
        - "anthropic/"
        - "google/"
      require_text_output: true         # exclude non-text models
      require_tools_support: false      # exclude models without tools
      openrouter_api_key: "sk-or-..."  # your OpenRouter API key
      openrouter_base_url: "https://openrouter.ai/api/v1"
      provider_name: "openrouter"       # CPA openai-compatibility provider name
      management_key: ""               # CPA mgmt key (empty = use MANAGEMENT_PASSWORD env)
      cpa_base_url: "http://localhost:8317"
```

### Prerequisites

Your CPA config must have an `openai-compatibility` provider entry with `name: openrouter`:

```yaml
openai-compatibility:
  - name: openrouter
    base-url: "https://openrouter.ai/api/v1"
    api-key-entries:
      - api-key: "sk-or-..."
    models: []  # will be populated by this plugin
```

The plugin updates the `models` array via the Management API. All other provider fields (base-url, api-key-entries, headers) are preserved.

## Web Panel

After installation, access the panel at:

```
http://localhost:8317/v0/resource/plugins/openrouter-free-sync/panel
```

From here you can:
- View last sync status and synced models
- Edit filter configuration
- Trigger manual refresh

## Management API Routes

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/v0/management/plugins/openrouter-free-sync/refresh` | Trigger sync now |
| `GET` | `/v0/management/plugins/openrouter-free-sync/status` | Last sync result |
| `GET` | `/v0/management/plugins/openrouter-free-sync/config` | Current config |
| `PUT` | `/v0/management/plugins/openrouter-free-sync/config` | Update config |

## Build

```bash
# Single platform
make build

# All platforms (requires cross-compilation toolchains)
make build-all
```

Artifacts are written to `dist/` with checksums.

## Requirements

- CPA v7+ with plugin support (`X-CPA-SUPPORT-PLUGIN: 1`)
- Go 1.24+ with CGO enabled
- OpenRouter API key
- CPA management key (via config or `MANAGEMENT_PASSWORD` env var)

## License

MIT
