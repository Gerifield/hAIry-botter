# hAIry Botter

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

### Running the server

Run the server:
```
go run cmd/bot-server/main.go
```

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


All the history will be stored under the `history-gemini` folder.

### Notes

Please do not run this server publicly available for your own safety. (And for your budget, if it is public, anybody can use it and it can quickly add up in the Gemini API usage.)
It is intended to be an "internal" helper for devs.
