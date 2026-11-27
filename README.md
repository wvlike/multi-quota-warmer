# Multi-Provider Five-Hour Quota Warmer (`multi-quota-warmer`)

`multi-quota-warmer` is a Go dynamic-library plugin for CLIProxyAPI. It sends one non-streaming OpenAI-compatible message through active **Codex** and **Antigravity** credentials at configured daily times to activate their five-hour quota reset windows. Each request uses the exact credential ID returned by `host.auth.list`.

The plugin selects credentials where all of the following are true:

- `provider` is an enabled target provider (`codex` or `antigravity`);
- `disabled` is `false`;
- `id` is present.

The plugin reports `status` and `unavailable` in the selection log but does not use them to skip an enabled credential. CPA evaluates cooldown and availability for the configured warm-up model when the request is sent.

## Management UI

The plugin registers a browser resource at:

```text
/v0/resource/plugins/multi-quota-warmer/status
```

The page provides:

- Manual warm-up buttons for **All Enabled Providers**, **Codex Only**, or **Antigravity Only**;
- Independent enable switch, model, and warm-up prompt for **Codex** and **Antigravity**;
- Shared editable daily schedule rows, next scheduled execution times, and latest credential-level results (with provider column);
- Paginated and filterable runtime logs (page size selector, level filter, keyword search, and page navigation).

## Configuration

```yaml
plugins:
  enabled: true
  dir: "plugins"
  configs:
    multi-quota-warmer:
      enabled: true
      priority: 1
      codex:
        enabled: true
        model: "gpt-5.6-luna"
        prompt: "Reply with exactly: quota window activated"
      antigravity:
        enabled: true
        model: "gemini-3-flash"
        prompt: "Explain how HTTP/2 stream multiplexing works in detail with concrete examples. Write at least 600 tokens across several paragraphs without summarizing early."
      timezone: "Asia/Singapore"
      max_log_entries: 200
      state_file: "plugins/state/multi-quota-warmer.json"
      schedules:
        - id: "morning-05"
          at: "05:00"
          enabled: true
        - id: "morning-10"
          at: "10:00"
          enabled: true
        - id: "afternoon-15"
          at: "15:00"
          enabled: true
```

> **Note on Antigravity Gemini models**: Google Antigravity's Gemini 5h window only registers usage reliably when the completion generates substantial output (roughly >= 600 tokens). The default Antigravity prompt requests a detailed >= 600-token explanation; if you configure a Claude model on Antigravity (e.g. `claude-sonnet-5-5-high`), a short prompt is sufficient.

For backward compatibility, legacy top-level `model` and `prompt` fields in YAML/JSON are automatically migrated into `codex.model` and `codex.prompt` if the `codex` sub-fields are omitted.

## Plugin Management API

All routes below require the CPA Management key:

```text
GET  /v0/management/plugins/multi-quota-warmer/status
POST /v0/management/plugins/multi-quota-warmer/run
GET  /v0/management/plugins/multi-quota-warmer/logs
```

- `POST /run` accepts an optional JSON body `{"provider": "all" | "codex" | "antigravity"}` (defaults to `"all"`).
- `GET /logs` supports optional query parameters: `page`, `page_size` (1–200), `level` (`info`|`warn`|`error`), and `q` (keyword filter). When `page` and `page_size` are omitted, all stored logs are returned along with `total`, `page`, `page_size`, and `total_pages`.

## Build and Install

From this repository on Linux, build the shared library:

```bash
cd go
CGO_ENABLED=1 go build -buildmode=c-shared -trimpath -ldflags="-s -w" -o ../multi-quota-warmer.so .
```

Install `multi-quota-warmer.so` (or `.dylib` on macOS, `.dll` on Windows) into the proxy server's `plugins/` or `plugins/<GOOS>/<GOARCH>/` directory, enable `multi-quota-warmer` in `config.yaml`, and restart CLIProxyAPI.
