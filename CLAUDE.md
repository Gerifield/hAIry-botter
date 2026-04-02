# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Build
go build ./...

# Run main server
go run cmd/server-bot/main.go

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

hairy-botter is a **modular AI chatbot server** built with Firebase Genkit. HTTP clients (Telegram, CLI, WhatsApp, etc.) communicate with the main server over a REST API. The server handles sessions, RAG retrieval, history, and delegates to Genkit for LLM generation with agentic tool use.

### Request flow

```
Client (HTTP POST /message)
  → internal/server   — extracts session ID, files, message
  → internal/ai/agent — reads history, retrieves RAG context, calls genkit.Generate()
                         Genkit handles the agentic loop (tool calls, MCP, Google Search)
  → internal/history  — saves updated conversation (resp.History())
  → returns text response
```

### Key packages

| Package | Role |
|---|---|
| `cmd/server-bot` | Entry point — wires all dependencies, reads env vars |
| `internal/ai/agent` | Core AI logic: history + RAG + Genkit generate |
| `internal/ai/gemini` | Genkit plugin setup (model, embedder, Google Search config) |
| `internal/ai/gemini-embedding` | Adapter: Genkit embedder → chromem `EmbeddingFunc` |
| `internal/server` | Chi HTTP router, session cookies, multipart file handling |
| `internal/history` | JSON session history; summarizes when length exceeds threshold |
| `internal/rag` | chromem-go vector DB — loads `bot-context/` docs on startup |
| `cmd/server-mcp` | Standalone MCP protocol server |
| `cmd/server-mcp-skills` | Sandboxed skills MCP server (file I/O, shell commands) |

### MCP tool pattern

Tools are registered with:
```go
genkit.DefineTool(g, name, desc, fn, ai.WithInputSchema(schema))
```
The tool `fn` signature is `func(tc *ai.ToolContext, input any) (any, error)` — `tc` embeds `context.Context`. Genkit handles the agentic tool-call loop automatically. **Do not call `gemini.New()` twice with overlapping tool names** — tool registration is global per Genkit instance.

### Session & history

- Session ID comes from a cookie or `X-User-ID` header.
- History is stored as `[]*ai.Message` (genkit type) in `history-gemini/<sessionID>.json`.
- Field name in JSON is `content` (not `parts` — old genai format is incompatible).
- When history length hits `HISTORY_SUMMARY` (default 20), a summary is generated and replaces the messages.

### Configuration (environment variables)

| Variable | Default | Purpose |
|---|---|---|
| `GEMINI_API_KEY` | — | **Required.** Google AI API key |
| `ADDR` | `:8080` | Listen address |
| `GEMINI_MODEL` | `gemini-2.5-flash` | Model name |
| `MCP_SERVERS` | — | Comma-separated MCP HTTP server URLs |
| `GEMINI_SEARCH_DISABLED` | `false` | Disable Google Search grounding |
| `HISTORY_SUMMARY` | `20` | Summarization threshold (message count) |
| `LOG_LEVEL` | `info` | `debug`/`info`/`warn`/`error` |
| `CORS_ALLOWED_ORIGIN` | `*` | CORS origin |

**File-based config** (loaded from working directory at startup):
- `personality.txt` — system prompt (supports both `{"text":"..."}` and `{"parts":[...]}` JSON formats)
- `bot-context/` — documents auto-indexed into RAG vector DB

### Genkit specifics

- `genkit.Init` panics on failure (does not return error).
- For known Gemini models: `ga.DefineModel(g, name, nil)`.
- For unknown models: provide `*ai.ModelOptions{Supports: &googlegenai.Multimodal}`.
- Google Search grounding is Gemini-specific and passed via `ai.WithConfig(&genai.GenerateContentConfig{Tools: ...})`.
- The genkit fork used is `gerifield/genkit/go v1.5.0-fix` (replace directive in go.mod).
