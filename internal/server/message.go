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
	userID := r.Header.Get("X-User-ID") // Optionally pass userID in header

	if userID == "" { // No userID in header, use a cookie or create one if needed
		sessionCookie, err := r.Cookie(sessionCookieName)
		if err != nil {
			// Cookie not found, create one
			sessionCookie = &http.Cookie{
				Name:  sessionCookieName,
				Value: s.genSessionID(),
			}

			http.SetCookie(w, sessionCookie)
		}
		userID = sessionCookie.Value
	}

	res, err := s.logic.HandleMessage(r.Context(), userID, msg)
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
