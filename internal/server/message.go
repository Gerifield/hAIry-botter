package server

import (
	"crypto/rand"
	"encoding/json"
	"net/http"

	"hairy-botter/internal/ai/domain"
)

const sessionCookieName = "sessionID"

func (s *Server) genSessionID() string {
	return rand.Text()
}

func (s *Server) postMessage(w http.ResponseWriter, r *http.Request) {
	msg := r.PostFormValue("message")
	userID := r.Header.Get("X-User-ID") // Optionally pass userID in header

	var img *domain.Image
	imgReader, imgHeader, err := r.FormFile("image")
	if err == nil {
		data := make([]byte, imgHeader.Size)
		if _, err := imgReader.Read(data); err != nil {
			http.Error(w, "failed to read image data", http.StatusInternalServerError)
			return
		}
		img = &domain.Image{
			MimeType: imgHeader.Header.Get("Content-Type"),
			Data:     data,
		}
	}

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

	res, err := s.logic.HandleMessage(r.Context(), userID, domain.Request{
		Message: msg,
		Image:   img,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)

		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	_ = json.NewEncoder(w).Encode(struct {
		Response string `json:"response"`
	}{
		Response: res,
	})
}
