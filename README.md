<div align="center">

<img src="https://github.com/user-attachments/assets/10e49300-eb17-41a3-b8c9-affd399c8810" width="250" />

# hAIry Botter 🪄 ✨

**A flexible, HTTP-based AI Chatbot Server powered by Gemini.**

[![Go Report Card](https://goreportcard.com/badge/github.com/yourusername/hairy-botter)](https://goreportcard.com/report/github.com/yourusername/hairy-botter)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Powered By Gemini](https://img.shields.io/badge/AI-Gemini-blue)](https://deepmind.google/technologies/gemini/)

</div>

---

## 📖 Overview

**hAIry Botter** is a lightweight, backend-agnostic AI server designed to decouple the AI logic from the frontend. Inspired by the [WhatsApp Python Chatbot](https://github.com/YonkoSam/whatsapp-python-chatbot), this project aims to be more flexible by offering a simple HTTP API that supports history, context, and external tools.

Whether you are building a CLI, a Telegram bot, or a web interface, you just need to make a simple HTTP call to hAIry Botter to get started.

## ✨ Features

* 🧠 **Gemini Powered:** Uses the latest Google Gemini models.
* 🔌 **MCP Support:** Implements the **Model Context Protocol** to call external servers/functions (includes example implementation).
* 💾 **Smart History:** Session-based history storage (`history-gemini` folder) with optional auto-summarization to save context window.
* 📚 **RAG Capable:** Built-in Retrieval-Augmented Generation. Drop text documents into the `bot-context` folder to chat with your data.
* 🎭 **Custom Personality:** Configurable base prompts via `personality.json`.
* 🖼️ **Multi-modal:** Native support for Image and PDF inputs.
* 🚀 **Ready-to-use Clients:** Includes CLI, Telegram, and Facebook Messenger clients.

---

## 🚀 Quick Start

### Option 1: Docker (Recommended)

The easiest way to get up and running is via Docker Compose.

1.  Copy `.env.example` to `.env`.
2.  Set your `GEMINI_API_KEY` in the file.
3.  Run the stack:

```bash
docker-compose up
```

### Option 2: Running from Source

**Prerequisites:** Go installed on your machine.

1.  Set the required environment variable:
    ```bash
    export GEMINI_API_KEY="your_api_key_here"
    ```
2.  Run the server:
    ```bash
    go run cmd/bot-server/main.go
    ```

---

## ⚙️ Configuration

You can configure the server using Environment Variables.

| Variable | Description | Default | Required |
| :--- | :--- | :--- | :---: |
| `GEMINI_API_KEY` | Your Google Gemini API access key. | - | ✅ |
| `ADDR` | Server listen address. | `:8080` | ❌ |
| `GEMINI_MODEL` | The specific model version to use. | `gemini-2.5-flash` | ❌ |
| `MCP_SERVERS` | Comma-separated list of MCP HTTP stream servers (e.g., `http://localhost:8081/mcp`). | - | ❌ |
| `SEARCH_ENABLE` | Allow Google Search (Warning: conflicts with MCP). | `false` | ❌ |
| `HISTORY_SUMMARY` | Message count trigger for history summarization (`0` to disable). | `20` | ❌ |
| `LOG_LEVEL` | Logging verbosity (`debug`, `info`, `warn`, `error`). | `info` | ❌ |

> **Note on MCP:** You cannot use the same function name across different MCP servers. Since functions are mapped to clients, duplicate names will override previous ones.

---

## 📡 API Usage

The server exposes a simple HTTP endpoint.

### 1. New Conversation (No Session)
If you don't provide a User ID, the server generates a new session and returns it in a cookie.

```bash
curl -v -X POST [http://127.0.0.1:8080/message](http://127.0.0.1:8080/message) \
  -d "message=Hi there"
```

### 2. Continued Conversation (With Session)
To maintain history, pass the `sessionID` cookie returned from the first call.

```bash
curl -v -X POST \
  -H "Cookie: sessionID=MGVQOSOZWPMKWAJBQN5KWFR3DF" \
  [http://127.0.0.1:8080/message](http://127.0.0.1:8080/message) \
  -d "message=Hi again"
```

### 3. Using a Custom User ID
If your frontend manages users, pass the ID via header.

```bash
curl -v -X POST \
  -H "X-User-ID: unique-user-123" \
  [http://127.0.0.1:8080/message](http://127.0.0.1:8080/message) \
  -d "message=Hi there"
```

### 4. Multi-modal (Images & PDFs)
Send files using `multipart/form-data`.

```bash
curl -v -X POST \
  -F "message=What is on this image?" \
  -F "payload=@local_image.jpg" \
  [http://127.0.0.1:8080/message](http://127.0.0.1:8080/message)
```

---

## 📱 Included Clients

This repo comes with ready-made clients to demonstrate capabilities.

### 🖥️ CLI Client
An interactive terminal chat.

```bash
# Optional: Set SERVER_URL if not using localhost:8080
go run cmd/cli-client/main.go
```
![cli-client](examples/client-cli-demo.svg)

### ✈️ Telegram Bot
Requires a Bot Token from BotFather.

```bash
export BOT_TOKEN="your_telegram_token"
# Optional: restrict access to specific usernames
export USERNAME_LIMITS="user1,user2" 

go run cmd/client-telegram/main.go
```
*Tip: Captions on images are treated as the prompt.*

### 💬 Facebook Messenger
Requires a configured Facebook App/Page.

**Env Variables:**
* `ACCESS_TOKEN`, `VERIFY_TOKEN`, `APP_SECRET` (Required)
* `ADDR` (Default: `:8082`)
* `AI_SERVICE` (Default: `http://127.0.0.1:8080`)

```bash
go run cmd/client-messenger/main.go
```
*Tip: Use `ngrok http 8082` to expose this to Facebook for local testing.*

---

## ⚠️ Important Notes

> **Security Warning:** Please do not run this server on the public internet without additional authentication. It is intended as an internal helper tool. Public exposure could lead to excessive API usage and costs.

> **💡 Pro Tip:** If you add a **Shell MCP server**, you can add "OpenClaw skills" into the RAG processing folder. These "skills" are text files that become part of the prompt, allowing the AI to execute shell-based function calls!
