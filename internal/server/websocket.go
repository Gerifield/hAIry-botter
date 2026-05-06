package server

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"

	"hairy-botter/internal/ai/domain"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/go-chi/chi/v5"
)

type SafeConn struct {
	mu   sync.Mutex
	conn *websocket.Conn
}

// ConnectionManager manages active WebSocket connections by session ID.
type ConnectionManager struct {
	mu          sync.RWMutex
	connections map[string][]*SafeConn
}

// NewConnectionManager creates a new ConnectionManager.
func NewConnectionManager() *ConnectionManager {
	return &ConnectionManager{
		connections: make(map[string][]*SafeConn),
	}
}

// Add adds a new connection for the given session ID.
func (cm *ConnectionManager) Add(sessionID string, conn *websocket.Conn) *SafeConn {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	safeConn := &SafeConn{conn: conn}
	cm.connections[sessionID] = append(cm.connections[sessionID], safeConn)
	return safeConn
}

// Remove removes a connection for the given session ID.
func (cm *ConnectionManager) Remove(sessionID string, safeConn *SafeConn) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	conns := cm.connections[sessionID]
	for i, c := range conns {
		if c == safeConn {
			// Remove from slice
			cm.connections[sessionID] = append(conns[:i], conns[i+1:]...)
			break
		}
	}

	// Clean up empty slices
	if len(cm.connections[sessionID]) == 0 {
		delete(cm.connections, sessionID)
	}
}

// Broadcast sends a JSON message to all connections for a given session ID.
func (cm *ConnectionManager) Broadcast(ctx context.Context, sessionID string, message interface{}) {
	cm.mu.RLock()
	conns := cm.connections[sessionID]
	// Copy slice to avoid data race
	connsCopy := make([]*SafeConn, len(conns))
	copy(connsCopy, conns)
	cm.mu.RUnlock()

	// Convert message to JSON bytes once
	b, err := json.Marshal(message)
	if err != nil {
		return
	}

	for _, safeConn := range connsCopy {
		safeConn.mu.Lock()
		_ = safeConn.conn.Write(ctx, websocket.MessageText, b)
		safeConn.mu.Unlock()
	}
}

type WSRequest struct {
	Message    string         `json:"message"`
	InlineData []WSInlineData `json:"inline_data,omitempty"`
}

type WSInlineData struct {
	MimeType string `json:"mime_type"`
	Data     []byte `json:"data"`
}

type WSResponse struct {
	Response string `json:"response"`
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "session_id")
	if sessionID == "" {
		http.Error(w, "missing session_id", http.StatusBadRequest)
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"*"}, // Adjust as necessary based on CORS settings
	})
	if err != nil {
		// Log error, but Accept already writes to the ResponseWriter
		return
	}
	defer conn.CloseNow()

	safeConn := s.cm.Add(sessionID, conn)
	defer s.cm.Remove(sessionID, safeConn)

	ctx := r.Context()

	for {
		var wsReq WSRequest
		err := wsjson.Read(ctx, conn, &wsReq)
		if err != nil {
			// Typically an expected close or connection dropped
			break
		}

		var inlineData []*domain.InlineData
		for _, data := range wsReq.InlineData {
			inlineData = append(inlineData, &domain.InlineData{
				MimeType: data.MimeType,
				Data:     data.Data,
			})
		}

		req := domain.Request{
			Message:    wsReq.Message,
			InlineData: inlineData,
		}

		// Run HandleMessage in a separate goroutine so we don't block the read loop
		go func(sessionID string, req domain.Request) {
			res, err := s.logic.HandleMessage(context.Background(), sessionID, req)
			if err != nil {
				s.cm.Broadcast(context.Background(), sessionID, WSResponse{Response: "Error: " + err.Error()})
				return
			}
			s.cm.Broadcast(context.Background(), sessionID, WSResponse{Response: res})
		}(sessionID, req)
	}
}
