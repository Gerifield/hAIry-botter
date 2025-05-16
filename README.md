# hAIry Botter

<img src="https://github.com/user-attachments/assets/10e49300-eb17-41a3-b8c9-affd399c8810" width=250 />

Chatbots are useful, especially after reading about the [https://github.com/YonkoSam/whatsapp-python-chatbot](https://github.com/YonkoSam/whatsapp-python-chatbot) on Reddit.
I thought why not create a bit more flexible one, not hardcoding to any kind of frontend.

So this project was born, a simple HTTP based server, with history storing.

There could be many improvements for example:
- Add more AI backends
- Add a better history store
- Add actually usable (chat) frontends

Right now you just need a frontend for any kind of chat and you can call this stuff.

Happy playing!

## Usage

### Pre-requisites

Required env variable(s):
- `GEMINI_API_KEY` - For the Gemini API access

Optional env variable(s):
- `ADDR` - Listen address for the server (Default: `:8080`)
- `GEMINI_MODEL` - Model to use (Default: `gemini-2.5-flash-preview-04-17`)
- `MCP_SSE_SERVER` - MCP SSE server for external function calls (Eg.: `http://localhost:8081/sse`)

All the history will be stored under the `history-gemini` folder.

### Running the server

Run the server from source:
```
go run cmd/bot-server/main.go
```

But feel free to download a pre-build package and then just run the binary. The env variables are still the same.

### Example call without unique user id:

If you don't have a user id, you can call the server without it. This will create a new session cookie and store an ID in it.

Example call for the server:
```
curl -v -X POST http://127.0.0.1:8080/message -d "message=Hi there"
```

This will return a cookie which will have a `sessionID`. You need to use this if you want to keep a history, for example:

```
curl -v -X POST -H "Cookie: sessionID=MGVQOSOZWPMKWAJBQN5KWFR3DF" http://127.0.0.1:8080/message -d "message=Hi there"
```

### Example call without unique user id:

If you have a user id, you can use it in the call.

```
curl -v -H "X-User-ID: someuserid1" -X POST http://127.0.0.1:8080/message -d "message=Hi there"
```


### Client examples

#### CLI client

You can use the CLI client to call the server. It should work out of the box, but there are some optional enviroment variables:
- `SERVER_URL` - Server base URL if it was changed from the port 8080

```
go run cmd/cli-client/main.go
```

This will lunch an interactive terminal to discuss with.

Feel free to use the generated binary in the release as well.

![cli-client](examples/client-cli-demo.svg)

### Notes

Please do not run this server publicly available for your own safety. (And for your budget, if it is public, anybody can use it and it can quickly add up in the Gemini API usage.)
It is intended to be an "internal" helper for devs.
