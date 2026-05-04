# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Build
go build ./...

# Run main server (auto-loads config.yaml from working directory)
go run cmd/server-bot/main.go

# Run with a different config file
go run cmd/server-bot/main.go -config path/to/config.yaml

# Test all packages
go test ./...

# Test a single package
go test ./internal/history/...

# Run a specific test
go test -run TestName ./internal/server/...

# Docker (recommended for full stack)
docker-compose up
```

## Architecture

hairy-botter is a **modular AI chatbot server** built with Firebase Genkit. It supports two run modes — a full `agent` mode with HTTP API and optional MCP server, and a lightweight `mcp_cli` mode that serves as a stateless stdio-based sub-agent. HTTP clients (Telegram, CLI, WhatsApp, etc.) communicate with the main server over a REST API. Configuration is file-based via `config.yaml`.

### Request flow (agent mode)

```
Client (HTTP POST /message)
  → internal/server   — extracts session ID, files, message
  → internal/ai/agent — reads history, retrieves RAG context, calls genkit.Generate()
                         Genkit handles the agentic loop (tool calls, MCP, Google Search)
  → internal/history  — saves updated conversation
  → returns text response
```

### Multi-agent / sub-agent flow

```
Orchestrator agent (agent mode, cheap model)
  → discovers sub-agents via MCP_SERVERS in its config.yaml
  └─ sub-agent A (mcp_cli mode, stdio transport — launched as child process)
  └─ sub-agent B (agent mode, enable_mcp_http: true — running on a port)
     Each sub-agent exposes: chat(message, session_id?) and info() MCP tools
     Tool descriptions embed the sub-agent's role + its own tool list
     so the orchestrator can route without an extra info() round-trip
```

### Key packages

| Package | Role |
|---|---|
| `cmd/server-bot` | Entry point — loads `config.yaml`, wires all dependencies |
| `internal/config` | YAML config loading with env var fallback for API keys |
| `internal/ai/agent` | Core AI logic: history + RAG + Genkit generate; exposes `Persona()`, `ToolNames()` |
| `internal/ai/adapters` | Genkit adapters: `NewEmbedder()` → `rag.EmbeddingFunc`, `NewSummarizer()` → `history.Summarizer` |
| `internal/ai/gemini` | Genkit plugin setup (model, embedder, Google Search + thinking config) |
| `internal/mcpserver` | MCP transport over `agent.Logic` — `Start()` (HTTP) or `StartStdio()` (CLI) |
| `internal/server` | Chi HTTP router, session cookies, multipart file handling |
| `internal/history` | JSON session history; summarizes when length exceeds threshold |
| `internal/rag` | chromem-go vector DB — loads directory configured in `capabilities.rag.directory` |
| `cmd/server-mcp-skills` | Sandboxed skills MCP server (file I/O, shell commands) |

### Configuration (`config.yaml`)

All configuration lives in `config.yaml`. Pass a different path with `-config <path>`. Copy `config.yaml.example` to start.

#### Top-level fields

| Field | Default | Purpose |
|---|---|---|
| `run_mode` | `agent` | `agent` — full server; `mcp_cli` — stdio sub-agent |
| `model` | `gemini-pro-latest` | Gemini model name |
| `gemini_search_disabled` | `false` | Disable Google Search grounding (Gemini-specific) |
| `gemini_thinking_level` | — | Thinking level for supported models (`minimal`, etc.) |
| `log_level` | `warning` | `debug` / `info` / `warning` / `error` |

#### `agent_config` (evaluated only when `run_mode: agent`)

| Field | Default | Purpose |
|---|---|---|
| `enable_chat_proxy` | `true` | Enable the HTTP `/message` endpoint |
| `http_port` | `:8080` | HTTP listen address |
| `cors_allowed_origin` | `*` | CORS origin |
| `cors_allowed_methods` | `POST, OPTIONS` | CORS methods |
| `cors_allowed_headers` | `Content-Type, X-User-ID` | CORS headers |
| `enable_mcp_http` | `false` | Expose this agent as an MCP server over HTTP |
| `mcp_port` | `:8081` | MCP HTTP listen address |
| `agent_name` | `hairy-botter-agent` | MCP server name advertised during capability negotiation |
| `agent_description` | first line of persona | Short role description embedded in the `chat` tool description |

#### `personality`

| Field | Purpose |
|---|---|
| `role` | Short role label (e.g. `"Senior Go Developer"`) |
| `system_prompt` | Full system prompt text |

The concatenation of `role + "\n" + system_prompt + auto_inject_content` is the effective system prompt sent to the model. `agent_description` defaults to the first non-empty line of this combined string if not set explicitly.

#### `capabilities`

```yaml
capabilities:
  rag:
    enabled: true
    directory: "./bot-context"   # documents auto-indexed into chromem-go on startup
  history_summary:
    enabled: true
    message_count: 20            # summarise when history hits this length; 0 = disabled
  mcp_servers:
    - type: http                 # type inferred from path if omitted: http* → http, else cli
      path: http://localhost:8082/mcp
    - type: cli                  # launched as child process, communicates over stdio
      path: "go"
      args: ["run", "cmd/server-mcp-skills/main.go"]
      env:
        BASE_DIR: "/workspace"
```

`type` is optional — if the path starts with `http` the type defaults to `http`, otherwise `cli`.

#### `context.auto_inject`

```yaml
context:
  auto_inject:
    - "TODO.md"
    - "memory.md"
```

Files listed here are read from disk at startup and appended to the system prompt as `[System Context - File: <name>]` blocks. Leave the list empty for stateless sub-agents to save context space.

#### `api_keys`

```yaml
api_keys:
  gemini: "your_key_here"   # overridden by GEMINI_API_KEY env var if blank
```

`GEMINI_API_KEY` env var is the fallback when `api_keys.gemini` is not set in the file.

### Run modes

#### `agent` (default)

Full server. Starts the HTTP `/message` endpoint (if `enable_chat_proxy: true`) and optionally an MCP HTTP server (if `enable_mcp_http: true`). Both can run simultaneously.

#### `mcp_cli`

Stateless stdio sub-agent. No HTTP server is started. The process speaks MCP JSON-RPC over stdin/stdout and is intended to be launched as a child process by an orchestrator's `cli`-type MCP server entry. Logs go to stderr so they don't corrupt the stdio transport.

Typical orchestrator config to wire in a cli sub-agent:
```yaml
capabilities:
  mcp_servers:
    - type: cli
      path: "./my-subagent-binary"   # or "go run cmd/server-bot/main.go"
      args: ["-config", "subagent.yaml"]
```

### MCP sub-agent tools

Every agent running as an MCP server (either mode) exposes:

| Tool | Parameters | Description |
|---|---|---|
| `chat` | `message` (required), `session_id` (optional) | Send a message; get a response. Session ID generated per-call if omitted, preserving isolation. |
| `info` | — | Returns agent name, role description, and tool list as plain text. |

The `chat` tool description is dynamically built from `agent_name`, `agent_description`, and the list of MCP tools the sub-agent itself has access to — so an orchestrator's model can read routing context from tool discovery alone.

### Session & history

- In HTTP mode: session ID comes from the `sessionID` cookie or `X-User-ID` header.
- In MCP mode: session ID comes from the `session_id` tool parameter (UUID generated per call if omitted).
- History is stored as `[]*ai.Message` (genkit type) in `history-gemini/<sessionID>.json`.
- JSON field is `content` (not `parts` — old genai SDK format is incompatible; delete old files when upgrading).
- Only `[user message, model response]` pairs are saved — RAG context docs are intentionally excluded to avoid bloating history and summaries.

### Genkit specifics

- `genkit.Init` panics on failure (does not return error).
- For known Gemini models: `ga.DefineModel(g, name, nil)`. For unknown models: provide `*ai.ModelOptions{Supports: &googlegenai.Multimodal}`.
- Google Search grounding and MCP tools can be active simultaneously on Gemini 2.5+ models.
- Google Search grounding is Gemini-specific; passed via `ai.WithConfig(&genai.GenerateContentConfig{...})` inside `gemini.GenerateOptions()`.
- The genkit fork used is `gerifield/genkit/go v1.5.0-fix` (replace directive in go.mod).
- Tool registration is global per Genkit instance — do not register the same tool name twice.
