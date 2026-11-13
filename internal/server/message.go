package server

import (
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"hairy-botter/internal/ai/domain"
)

const sessionCookieName = "sessionID"

func (s *Server) genSessionID() string {
	return rand.Text()
}

func (s *Server) postMessage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", s.cfg.AllowedOrigin)
	w.Header().Set("Access-Control-Allow-Methods", s.cfg.AllowedMethods)
	w.Header().Set("Access-Control-Allow-Headers", s.cfg.AllowedHeaders)

	userID := r.Header.Get("X-User-ID") // Optionally pass userID in header

	var msg string
	var inlineData []*domain.InlineData

	contentType := strings.ToLower(r.Header.Get("Content-Type"))
	if strings.HasPrefix(contentType, "application/json") {
		var jsonReq struct {
			Message string `json:"message"`
		}
		if err := json.NewDecoder(r.Body).Decode(&jsonReq); err != nil {
			http.Error(w, "invalid json payload", http.StatusBadRequest)
			return
		}
		msg = jsonReq.Message
	} else {
		msg = r.PostFormValue("message")
		if err := r.ParseMultipartForm(32 << 20); err == nil && r.MultipartForm != nil {
			for _, fileHeaders := range r.MultipartForm.File {
				for _, binHeader := range fileHeaders {
					binReader, err := binHeader.Open()
					if err != nil {
						http.Error(w, "failed to open payload file", http.StatusInternalServerError)
						return
					}
					data, err := io.ReadAll(binReader)
					_ = binReader.Close()
					if err != nil {
						http.Error(w, "failed to read binary data", http.StatusInternalServerError)
						return
					}
					inlineData = append(inlineData, &domain.InlineData{
						MimeType: binHeader.Header.Get("Content-Type"),
						Data:     data,
					})
				}
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
	_ = json.NewEncoder(w).Encode(struct {
		Response string `json:"response"`
	}{
		Response: res,
	})
}
