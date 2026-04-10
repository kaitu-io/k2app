//go:build linux

package webui

import (
	"encoding/json"
	"net/http"
)

// NewStorageHandler returns an http.HandlerFunc for the /api/storage endpoint.
// The handler is a thin JSON wrapper over the given *Storage and implements
// six actions: get, set, remove, has, keys, clear.
//
// Request body: {"action": "<verb>", "key": "<name>", "value": <any>}
// Response body: {"code": 0|1, "message": "...", "data": <any>}
//
// For "get", the stored value is JSON-decoded before being returned so that
// objects round-trip as nested JSON rather than being double-escaped.
func NewStorageHandler(storage *Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
		var req struct {
			Action string `json:"action"`
			Key    string `json:"key"`
			Value  any    `json:"value"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, response{Code: 1, Message: "bad request"})
			return
		}
		switch req.Action {
		case "get":
			val, err := storage.Get(req.Key)
			if err != nil {
				writeJSON(w, response{Code: 0, Data: nil})
				return
			}
			var decoded any
			if json.Unmarshal([]byte(val), &decoded) == nil {
				writeJSON(w, response{Code: 0, Data: decoded})
			} else {
				writeJSON(w, response{Code: 0, Data: val})
			}
		case "set":
			data, _ := json.Marshal(req.Value)
			if err := storage.Set(req.Key, string(data)); err != nil {
				writeJSON(w, response{Code: 1, Message: err.Error()})
				return
			}
			writeJSON(w, response{Code: 0, Message: "ok"})
		case "remove":
			storage.Remove(req.Key)
			writeJSON(w, response{Code: 0, Message: "ok"})
		case "has":
			writeJSON(w, response{Code: 0, Data: storage.Has(req.Key)})
		case "keys":
			writeJSON(w, response{Code: 0, Data: storage.Keys()})
		case "clear":
			storage.Clear()
			writeJSON(w, response{Code: 0, Message: "ok"})
		default:
			writeJSON(w, response{Code: 1, Message: "unknown storage action"})
		}
	}
}
