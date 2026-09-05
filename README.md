# openrouter-free-sync

A [CLIProxyAPI (CPA)](https://github.com/router-for-me/CLIProxyAPI) plugin that automatically syncs free OpenRouter models into your CPA `openai-compatibility` provider config.

## Features

- **Auto-sync on a timer** — configurable interval (default: 24h), debounced startup (register bursts collapse into one sync)
- **Manual refresh** — one click from the web panel
- **Model detail tracking** — context length, modality, input/output pricing, tools support, per-model probe status
- **Availability probing** — each sync sends a 1-token request per model (rate-limited, default 4s apart to stay under OpenRouter's 20/min free limit):
  - **404** → quarantined immediately (model gone)
  - **N consecutive failures (429/5xx/timeout)** → quarantined after threshold (default 3)
  - **401/403** → treated as *restricted* (e.g. agent-harness-only models), not counted as failure
  - quarantined models are removed from CPA and re-probed at most every 30 min; automatic recovery on success
- **Auditable event log** — every change is recorded: `model_added` / `model_removed` / `model_quarantined` / `model_recovered` / `model_readded` / `sync` / `config_updated`, with before/after detail
- **State persistence** — models + audit log survive CPA restarts (`state_path`)
- **Configurable filters** — all via web UI: min context length, pricing filter (`free` = $0 in + $0 out), excluded providers, require text output, require tools
- **Web panel** — three tabs: Models (params + probe status), Audit Log (filterable), Config
- **Hot-reload** — config changes take effect immediately, no CPA restart
- **Panel edits persist across restarts** — non-secret config changes are saved to a
  config overlay in the state file and re-applied over config.yaml at startup;
  `PUT /settings` with `{"_reset_overlay": true}` restores config.yaml authority
- **Data hygiene** — audit log hard-capped (`audit_max_entries`); unchanged/error-free
  syncs are not audited unless `audit_sync_always: true`; inactive model records are
  pruned after `prune_after_days` (default 30); state file carries `schema_version`
- **Secret safety** — `GET /settings` returns masked secrets; masked echoes on `PUT`
  never overwrite real keys; secrets never enter the config overlay
- **Quota protection** — probe cooldown skips re-probing healthy models for
  `probe_cooldown_min` (default 10 min); restricted (401/403) models re-probed weekly;
  config changes abort any in-flight sync before it can PATCH stale results
- **Alias collision safety** — colliding auto-aliases fall back to full model IDs

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
      availability_check: true          # probe model availability each sync
      availability_fail_threshold: 3    # consecutive failures before quarantine
      probe_interval_ms: 4000           # delay between probes (stay under 20/min free limit)
      audit_max_entries: 500            # audit log capacity (hard cap)
      audit_sync_always: false          # audit unchanged syncs too (default off)
      prune_after_days: 30              # delete inactive model records after N days (0 = never)
      probe_cooldown_min: 10            # skip re-probing healthy models within N minutes
      state_path: "/CLIProxyAPI/state/orfs-state.json"  # persist state + audit log + config overlay
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
| `GET` | `/v0/management/plugins/openrouter-free-sync/status` | Counters + last sync |
| `GET` | `/v0/management/plugins/openrouter-free-sync/models` | Detailed model metadata + probe status |
| `GET` | `/v0/management/plugins/openrouter-free-sync/audit?limit=N` | Auditable event log |
| `GET` | `/v0/management/plugins/openrouter-free-sync/settings` | Config (secrets masked) |
| `PUT` | `/v0/management/plugins/openrouter-free-sync/settings` | Update config (persisted as overlay) |

> Note: `GET/PUT .../config` is intercepted natively by the CPA host (raw config.yaml
> view); plugins cannot override host routes, hence `/settings`.

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
