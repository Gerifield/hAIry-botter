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

	var inlineData []*domain.InlineData
	if err := r.ParseMultipartForm(32 << 20); err == nil {
		for _, fileHeaders := range r.MultipartForm.File {
			for _, binHeader := range fileHeaders {
				binReader, err := binHeader.Open()
				if err != nil {
					http.Error(w, "failed to open payload file", http.StatusInternalServerError)
					return
				}
				data := make([]byte, binHeader.Size)
				if _, err := binReader.Read(data); err != nil {
					_ = binReader.Close()
					http.Error(w, "failed to read binary data", http.StatusInternalServerError)
					return
				}
				_ = binReader.Close()
				inlineData = append(inlineData, &domain.InlineData{
					MimeType: binHeader.Header.Get("Content-Type"),
					Data:     data,
				})
			}
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
		Message:    msg,
		InlineData: inlineData,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)

		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", s.cfg.AllowedOrigin)
	_ = json.NewEncoder(w).Encode(struct {
		Response string `json:"response"`
	}{
		Response: res,
	})
}
