package utils

import (
	"encoding/json"
	"net/http"
)

type APIResponse struct {
	Code    int         `json:"code"`
	Data    interface{} `json:"data,omitempty"`
	Message string      `json:"message"`
}

func WriteJSON(w http.ResponseWriter, status int, code int, message string, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(APIResponse{Code: code, Data: data, Message: message})
}

func Error(w http.ResponseWriter, status int, message string) {
	WriteJSON(w, status, status, message, nil)
}
