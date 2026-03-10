package main

import (
	_ "embed"
	"net/http"
)

//go:embed ui.html
var uiHTML []byte

func (h *Handler) handleUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(uiHTML)
}
