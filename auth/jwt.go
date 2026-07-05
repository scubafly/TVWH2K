// Package auth verifies Supabase-issued JWTs on authenticated API routes.
package auth

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

type contextKey string

const userIDContextKey contextKey = "userID"

// Verifier checks the signature/expiry of Supabase JWTs using the project's
// JWT secret (Settings -> API -> JWT Secret in the Supabase dashboard).
//
// Note: this assumes a Supabase project using the legacy shared HS256 secret.
// Projects created after Supabase's move to asymmetric signing keys need
// JWKS-based (RS256/ES256) verification instead -- swap this out if so.
type Verifier struct {
	secret []byte
}

func NewVerifierFromEnv() (*Verifier, error) {
	secret := os.Getenv("SUPABASE_JWT_SECRET")
	if secret == "" {
		return nil, fmt.Errorf("SUPABASE_JWT_SECRET not set")
	}
	return &Verifier{secret: []byte(secret)}, nil
}

// UserIDFromToken parses and validates a Supabase access token, returning the
// user's UUID (the JWT's "sub" claim).
func (v *Verifier) UserIDFromToken(tokenString string) (string, error) {
	claims := jwt.MapClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return v.secret, nil
	})
	if err != nil {
		return "", fmt.Errorf("invalid token: %w", err)
	}
	if !token.Valid {
		return "", fmt.Errorf("token not valid")
	}
	sub, ok := claims["sub"].(string)
	if !ok || sub == "" {
		return "", fmt.Errorf("token missing sub claim")
	}
	return sub, nil
}

// Middleware requires a valid "Authorization: Bearer <token>" header and
// stores the resolved user ID in the request context.
func (v *Verifier) Middleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			http.Error(w, "Missing bearer token", http.StatusUnauthorized)
			return
		}
		tokenString := strings.TrimPrefix(authHeader, "Bearer ")

		userID, err := v.UserIDFromToken(tokenString)
		if err != nil {
			http.Error(w, fmt.Sprintf("Unauthorized: %v", err), http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), userIDContextKey, userID)
		next(w, r.WithContext(ctx))
	}
}

// UserIDFromContext retrieves the user ID stored by Middleware.
func UserIDFromContext(ctx context.Context) (string, bool) {
	userID, ok := ctx.Value(userIDContextKey).(string)
	return userID, ok
}
