<div align="center">

<img src="https://github.com/user-attachments/assets/10e49300-eb17-41a3-b8c9-affd399c8810" width="250" />

# hAIry Botter 🪄 ✨

**A flexible, HTTP-based AI Chatbot Server powered by Firebase Genkit. Supports Gemini and OpenAI-compatible providers.**

[![Go Report Card](https://goreportcard.com/badge/github.com/yourusername/hairy-botter)](https://goreportcard.com/report/github.com/yourusername/hairy-botter)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Powered By Gemini](https://img.shields.io/badge/AI-Gemini-blue)](https://deepmind.google/technologies/gemini/)

</div>

---

## 📖 Overview

**hAIry Botter** is a lightweight, backend-agnostic AI server designed to decouple the AI logic from the frontend. Inspired by the [WhatsApp Python Chatbot](https://github.com/YonkoSam/whatsapp-python-chatbot), this project aims to be more flexible by offering a simple HTTP API that supports history, context, and external tools.

Whether you are building a CLI, a Telegram bot, or a web interface, you just need to make a simple HTTP call to hAIry Botter to get started.

## ✨ Features

* 🧠 **Genkit Powered:** Uses [Firebase Genkit](https://firebase.google.com/docs/genkit) as the AI framework. Provider is selectable via `config.yaml` — Gemini (default) and OpenAI (or any OpenAI-compatible endpoint) are supported out of the box.
* 🔌 **MCP Support:** Implements the **Model Context Protocol** to call external servers/functions via Genkit's MCP plugin (includes example Skills MCP server).
* 💾 **Smart History:** Session-based history storage (`history-gemini` folder) with optional auto-summarization to save context window.
* 📚 **RAG Capable:** Built-in Retrieval-Augmented Generation. Drop text documents into the `bot-context` folder to give the agent long-term, searchable knowledge. The embedder provider can be configured independently from the main AI provider.
* 🎭 **Custom Personality:** Role and system prompt defined directly in `config.yaml`.
* 🤖 **Multi-agent / Sub-agent:** Agents can expose themselves as MCP servers (HTTP or stdio) so an orchestrator can delegate tasks to specialised sub-agents, each with its own config, model, and tool set.
* 🖼️ **Multi-modal:** Native support for Image and PDF inputs.
* ⚡ **Command Output Caching:** Includes `cachefor`, a small CLI wrapper that caches command output for a configurable TTL — useful for injecting slow-changing dynamic data into the system prompt without re-running the command every request.
* 🚀 **Ready-to-use Clients:** Includes CLI, Telegram, Facebook Messenger, WhatsApp, and Gmail clients.

---

## 🚀 Quick Start

### Option 1: Docker (Recommended)

The easiest way to get up and running is via Docker Compose.

1.  Copy `config.yaml.example` to `config.yaml` and set your API key (e.g. `providers.gemini.api_key` or the `GEMINI_API_KEY` env var).
2.  Run the stack:

```bash
docker-compose up
```

### Option 2: Running from Source

**Prerequisites:** Go installed on your machine.

1.  Copy `config.yaml.example` to `config.yaml` and configure your provider and API key:
    ```yaml
    provider: "gemini"   # or "openai"
    providers:
      gemini:
        api_key: "your_gemini_api_key_here"
      # openai:
      #   api_key: "your_openai_api_key_here"
      #   base_url: ""   # optional; override for any OpenAI-compatible endpoint
    ```
    Alternatively, set the `GEMINI_API_KEY` or `OPENAI_API_KEY` environment variable — both are used as fallbacks when the key is absent from the file.
2.  Run the server (it auto-loads `config.yaml` from the working directory):
    ```bash
    go run cmd/server-bot/main.go
    ```

---

## ⚙️ Configuration

All configuration lives in `config.yaml`. Copy `config.yaml.example` to `config.yaml` and edit it. A different path can be supplied with `-config <path>`.

```yaml
run_mode: "agent"          # "agent" (HTTP server) or "mcp_cli" (stdio sub-agent)

# AI provider: "gemini" (default) or "openai" (any OpenAI-compatible endpoint)
provider: "gemini"
model: "gemini-flash-latest"   # gemini: e.g. "gemini-2.5-flash"; openai: e.g. "gpt-4o"
gemini_search_disabled: false  # Gemini-specific; ignored for other providers
gemini_thinking_level: "NONE"  # Gemini-specific; omit to use model default
log_level: "info"

personality:
  role: "Helpful assistant"
  system_prompt: "You are hAIry, a concise and friendly AI assistant."

agent_config:
  enable_chat_proxy: true   # expose POST /message
  http_port: ":8080"
  enable_mcp_http: false    # expose this agent as an MCP server
  mcp_port: ":8081"

capabilities:
  rag:
    enabled: true
    directory: "./bot-context"
    # embedder_provider: "gemini"   # defaults to top-level provider; can be different
    embedding_model: "gemini-embedding-001"
  history_summary:
    enabled: true
    message_count: 20
  mcp_servers:
    - type: http
      path: http://localhost:8082/mcp
    - type: cli                        # launched as child process via stdio
      path: "go"
      args: ["run", "cmd/server-mcp-skills/main.go"]
      env:                             # optional extra env vars for the subprocess
        BASE_DIR: "/workspace"

context:
  static_inject:            # files re-read and injected into the system prompt on every request
    - "TODO.md"
  dynamic_data:             # commands run on every request; output injected into the system prompt
    - name: "Current date"  # command only → runs via sh -c (supports pipes/redirects)
      command: "date"
    - name: "Weather"       # command + args → direct execution (handles spaces in args correctly)
      command: "weather-bin"
      args: ["--city", "New York"]
    - name: "Build info"    # wrap slow commands with cachefor to avoid re-running on every request
      command: "cachefor"
      args: ["-cacheTime", "10m", "--", "my-slow-command", "--flag"]

# Provider credentials — env vars GEMINI_API_KEY / OPENAI_API_KEY are also supported
providers:
  gemini:
    api_key: ""
  openai:
    api_key: ""
    base_url: ""  # optional; set to use any OpenAI-compatible endpoint
```

See `config.yaml.example` for the full reference with all options and comments.

> **Note on Providers:** Set `provider: "gemini"` (default) or `provider: "openai"`. For OpenAI-compatible endpoints (Azure, local Ollama with an OpenAI shim, etc.) set `providers.openai.base_url`. The embedder can use a different provider than the main model via `capabilities.rag.embedder_provider`.

> **Note on MCP:** Tools from each MCP server are automatically namespaced by their index (e.g. `mcp-0_chat`, `mcp-1_chat`), so identical tool names across different servers don't collide. The uniqueness constraint only applies to tools defined manually via `genkit.DefineTool`.

> **Note on Search + MCP:** Google Search grounding and MCP tools work simultaneously on Gemini 2.5+ models. Disable search with `gemini_search_disabled: true`.

> **Note on Thinking:** `gemini_thinking_level` controls the model's internal reasoning budget. `NONE` and `MINIMAL` map to the lowest setting and are only valid for Flash models (Pro models silently ignore them). Pro models support `LOW`, `MEDIUM`, and `HIGH`. Omit the field entirely to use the model's default budget.

---

## 📡 API Usage

The server exposes a simple HTTP endpoint.

### 1. New Conversation (No Session)
If you don't provide a User ID, the server generates a new session and returns it in a cookie.

```bash
curl -v -X POST http://127.0.0.1:8080/message \
  -d "message=Hi there"
```

### 2. Continued Conversation (With Session)
To maintain history, pass the `sessionID` cookie returned from the first call.

```bash
curl -v -X POST \
  -H "Cookie: sessionID=MGVQOSOZWPMKWAJBQN5KWFR3DF" \
  http://127.0.0.1:8080/message \
  -d "message=Hi again"
```

### 3. Using a Custom User ID
If your frontend manages users, pass the ID via header.

```bash
curl -v -X POST \
  -H "X-User-ID: unique-user-123" \
  http://127.0.0.1:8080/message \
  -d "message=Hi there"
```

### 4. Multi-modal (Images & PDFs)
Send files using `multipart/form-data`.

```bash
curl -v -X POST \
  -F "message=What is on this image?" \
  -F "payload=@local_image.jpg" \
  http://127.0.0.1:8080/message
```

---

## 📱 Included Clients

This repo comes with ready-made clients to demonstrate capabilities.

### 🖥️ CLI Client
An interactive terminal chat.

```bash
# Optional: Set SERVER_URL if not using localhost:8080
go run cmd/client-cli/main.go
```
![cli-client](examples/client-cli-demo.svg)

### ✈️ Telegram Bot
Requires a Bot Token from BotFather.

**Env Variables:**
* `BOT_TOKEN` (Required)
* `AI_SERVICE` (Default: `http://127.0.0.1:8080`)
* `USERNAME_LIMITS` (Optional, comma-separated — restrict access to specific usernames)
* `PORT` (Default: `8085`) — HTTP webhook port for push notifications to the bot

```bash
export BOT_TOKEN="your_telegram_token"
# Optional: restrict access to specific usernames
export USERNAME_LIMITS="user1,user2"

go run cmd/client-telegram/main.go
```
*Tip: Captions on images are treated as the prompt. The bot also exposes an HTTP endpoint (`POST /`) on `PORT` to forward messages into the active Telegram chat.*

### 💬 Facebook Messenger
Requires a configured Facebook App/Page.

**Env Variables:**
* `ACCESS_TOKEN`, `VERIFY_TOKEN`, `APP_SECRET` (Required)
* `ADDR` (Default: `:8082`)
* `AI_SERVICE` (Default: `http://127.0.0.1:8080`)

```bash
go run cmd/client-fb-messenger/main.go
```
*Tip: Use `ngrok http 8082` to expose this to Facebook for local testing.*

### 💬 WhatsApp
Requires a WhatsApp Business account and Graph API credentials.

**Env Variables:**
* `ACCESS_TOKEN`, `VERIFY_TOKEN`, `APP_SECRET`, `WHATSAPP_BUSINESS_PHONE_ID` (Required)
* `GRAPHQL_URL` (Optional — Meta Graph API base URL)
* `ADDR` (Default: `:8082`)
* `AI_SERVICE` (Default: `http://127.0.0.1:8080`)

```bash
go run cmd/client-whatsapp/main.go
```

### 📧 Gmail Reader
Polls a Gmail mailbox and forwards matching emails to the AI server as messages.

**Env Variables:**
* `WEBHOOK_URL` (Optional — AI server URL to forward emails to)
* `SEARCH_QUERY` (Optional — Gmail search filter, default targets `label:Assistant` or a specific address)
* `POLLING_INTERVAL` (Optional — polling frequency in seconds, default `60`)

```bash
go run cmd/gmail-reader/main.go
```
*Requires OAuth2 credentials for Gmail API access.*

---

## 🎭 Personality

The system prompt is defined directly in `config.yaml` under the `personality` section:

```yaml
personality:
  role: "Senior Go Developer"
  system_prompt: "You are an autonomous coding agent. Always check TODO.md before writing code."
```

Both fields are concatenated to form the base system prompt. Additional context is appended on every request via `context.static_inject` (files) and `context.dynamic_data` (commands). Dynamic commands run via `sh -c` when no `args` are given (supports pipes/redirects), or directly when `args` are provided (safer for arguments with spaces).

> **Note:** Previous versions used a separate `personality.txt` file. This has been removed — move your prompt into `config.yaml`.

---

## 💾 History Compatibility

History files are stored in the `history-gemini/` folder as JSON. After the migration from the raw `genai` SDK to Firebase Genkit, the internal message format changed (`parts` → `content`). **Old history files are not compatible** and should be deleted or the folder cleared before upgrading.

---

## 🛠️ Skills MCP Server

The repo includes a dedicated MCP (Model Context Protocol) server designed to give the AI agent autonomous access to a sandboxed environment. This allows the AI to run commands, edit code, and modify files similar to how tools like OpenDevin or OpenClaw work.

**Features & Tools:**
- `execute_command`: Execute arbitrary shell commands in the container.
- `list_files`: List files and directories within a given path.
- `read_file`: Read the contents of a specific file.
- `write_file`: Write or overwrite the contents of a file.

**Configuration (flags or env vars):**
* `-port` / `PORT` — listen port (default `8081`)
* `-base-dir` / `BASE_DIR` — sandbox root directory (default `.`)
* `-log-level` / `LOG_LEVEL` — log verbosity: `debug`, `info`, `warn`, `error` (default `info`)

**Disabling individual tools:**
* `-disable-list-files` / `DISABLE_LIST_FILES=true`
* `-disable-read-file` / `DISABLE_READ_FILE=true`
* `-disable-write-file` / `DISABLE_WRITE_FILE=true`
* `-disable-execute-command` / `DISABLE_EXECUTE_COMMAND=true`

**Running the Skills Server:**
To run the full stack with the Skills MCP Server enabled, use the dedicated compose file:

```bash
docker-compose -f docker-compose-skill.yml up
```

**Docker Environment:**
The Skills MCP Server runs in an Alpine Linux Docker container. The container runs as a non-root user (`agentuser`) for security, so operations that require root — such as installing packages with `apk` — are not available at runtime.

---

## ⚡ cachefor

`cachefor` is a small CLI wrapper that caches the stdout, stderr, and exit code of any command for a configurable TTL. It is bundled into the Skills MCP Docker image and is particularly useful in `dynamic_data` entries where the same slow command (e.g. a network lookup or a build step) would otherwise re-run on every request.

```bash
# Cache the output of a command for 10 minutes
cachefor -cacheTime 10m my-slow-command --arg value

# Or via env var
CACHE_TIME=10m cachefor my-slow-command --arg value
```

Stale cache files are automatically cleaned up on each invocation.

---

## ⚠️ Important Notes

> **Security Warning:** Please do not run this server on the public internet without additional authentication. It is intended as an internal helper tool. Public exposure could lead to excessive API usage and costs. Furthermore, running the **Skills MCP Server** gives the AI the ability to execute arbitrary shell commands inside its container. Do not expose this environment or grant it access to sensitive host directories.

> **💡 Pro Tip:** When using the **Skills MCP Server**, use `static_inject` to teach the AI how to use specific CLI tools or project structures by injecting plain-text "skill" files directly into the system prompt. RAG (`bot-context/`) is a good alternative when you have a larger knowledge base and want semantic search rather than injecting everything verbatim on every request.
