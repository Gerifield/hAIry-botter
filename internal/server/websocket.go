package server

import (
	"encoding/base64"
	"fmt"
	"net/http"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/go-chi/chi/v5"

	"hairy-botter/internal/ai/domain"
)

// WSMessage is the JSON envelope used on the WebSocket wire.
type WSMessage struct {
	Type    string `json:"type"`              // "message", "response", "error", "event"
	Content string `json:"content,omitempty"` // text payload
	Session string `json:"session,omitempty"` // echoed back in responses
}

// WSIncoming is a message sent by the client over WebSocket.
type WSIncoming struct {
	Message    string          `json:"message"`
	InlineData []wsInlineData  `json:"inline_data,omitempty"`
}

type wsInlineData struct {
	MimeType string `json:"mime_type"`
	DataB64  string `json:"data"` // base64-encoded bytes
}

func (s *Server) wsHandler(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "session_id")
	if sessionID == "" {
		http.Error(w, "session_id required", http.StatusBadRequest)
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // CORS is handled at a higher level
	})
	if err != nil {
		return
	}

	wsc := &wsConn{conn: conn}
	s.connMgr.register(sessionID, wsc)
	defer func() {
		s.connMgr.unregister(sessionID, wsc)
		conn.CloseNow()
	}()

	ctx := r.Context()
	for {
		var in WSIncoming
		if err := wsjson.Read(ctx, conn, &in); err != nil {
			return
		}

		var inlineData []*domain.InlineData
		for _, d := range in.InlineData {
			raw, err := base64.StdEncoding.DecodeString(d.DataB64)
			if err != nil {
				_ = wsc.send(ctx, WSMessage{Type: "error", Content: fmt.Sprintf("invalid base64: %v", err), Session: sessionID})
				continue
			}
			inlineData = append(inlineData, &domain.InlineData{
				MimeType: d.MimeType,
				Data:     raw,
			})
		}

		res, err := s.logic.HandleMessage(ctx, sessionID, domain.Request{
			Message:    in.Message,
			InlineData: inlineData,
		})
		if err != nil {
			_ = wsc.send(ctx, WSMessage{Type: "error", Content: err.Error(), Session: sessionID})
			continue
		}

		_ = wsc.send(ctx, WSMessage{Type: "response", Content: res, Session: sessionID})
	}
}
