package server

import (
	"crypto/rand"
	"encoding/json"
	"net/http"
)

const sessionCookieName = "sessionID"

func (s *Server) genSessionID() string {
	return rand.Text()
}

func (s *Server) postMessage(w http.ResponseWriter, r *http.Request) {
	msg := r.PostFormValue("message")

	// TODO: use a proper userID if available
	sessionCookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		// Cookie not found, create one
		sessionCookie = &http.Cookie{
			Name:  sessionCookieName,
			Value: s.genSessionID(),
		}

		http.SetCookie(w, sessionCookie)
	}

	res, err := s.logic.HandleMessage(r.Context(), sessionCookie.Value, msg)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)

		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Response string `json:"response"`
	}{
		Response: res,
	})
}
