package server

import (
	"encoding/json"
	"net/http"
)

func (s *Server) postMessage(w http.ResponseWriter, r *http.Request) {
	msg := r.PostFormValue("message")

	res, err := s.logic.HandleMessage(msg)
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
