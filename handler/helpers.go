package handler

import (
	"encoding/json"
	"net/http"

	"tvwh2k/auth"
)

// authUserID reads the user ID stored in the request context by
// auth.Verifier.Middleware.
func authUserID(r *http.Request) (string, bool) {
	return auth.UserIDFromContext(r.Context())
}

// writeJSON sends v as a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
