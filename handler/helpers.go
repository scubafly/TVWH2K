package handler

import (
	"net/http"

	"tvwh2k/auth"
)

// authUserID reads the user ID stored in the request context by
// auth.Verifier.Middleware.
func authUserID(r *http.Request) (string, bool) {
	return auth.UserIDFromContext(r.Context())
}
