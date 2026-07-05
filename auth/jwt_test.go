package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var testSecret = []byte("test-jwt-secret")

func signToken(t *testing.T, method jwt.SigningMethod, key interface{}, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(method, claims)
	s, err := token.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func validClaims() jwt.MapClaims {
	return jwt.MapClaims{
		"sub": "11111111-2222-3333-4444-555555555555",
		"exp": time.Now().Add(time.Hour).Unix(),
	}
}

func TestValidTokenReturnsSub(t *testing.T) {
	v := NewVerifier(testSecret)
	tok := signToken(t, jwt.SigningMethodHS256, testSecret, validClaims())
	sub, err := v.UserIDFromToken(tok)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sub != "11111111-2222-3333-4444-555555555555" {
		t.Fatalf("wrong sub: %q", sub)
	}
}

func TestWrongSecretRejected(t *testing.T) {
	v := NewVerifier(testSecret)
	tok := signToken(t, jwt.SigningMethodHS256, []byte("attacker-secret"), validClaims())
	if _, err := v.UserIDFromToken(tok); err == nil {
		t.Fatal("expected error for token signed with wrong secret")
	}
}

func TestExpiredTokenRejected(t *testing.T) {
	v := NewVerifier(testSecret)
	claims := validClaims()
	claims["exp"] = time.Now().Add(-time.Hour).Unix()
	tok := signToken(t, jwt.SigningMethodHS256, testSecret, claims)
	if _, err := v.UserIDFromToken(tok); err == nil {
		t.Fatal("expected error for expired token")
	}
}

func TestMissingSubRejected(t *testing.T) {
	v := NewVerifier(testSecret)
	claims := jwt.MapClaims{"exp": time.Now().Add(time.Hour).Unix()}
	tok := signToken(t, jwt.SigningMethodHS256, testSecret, claims)
	if _, err := v.UserIDFromToken(tok); err == nil {
		t.Fatal("expected error for token without sub claim")
	}
}

func TestNoneAlgorithmRejected(t *testing.T) {
	v := NewVerifier(testSecret)
	token := jwt.NewWithClaims(jwt.SigningMethodNone, validClaims())
	tok, err := token.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.UserIDFromToken(tok); err == nil {
		t.Fatal("expected error for alg=none token")
	}
}

func TestMiddlewareRequiresBearer(t *testing.T) {
	v := NewVerifier(testSecret)
	called := false
	h := v.Middleware(func(w http.ResponseWriter, r *http.Request) { called = true })

	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest("GET", "/api/connections", nil))
	if rec.Code != http.StatusUnauthorized || called {
		t.Fatalf("expected 401 without bearer, got %d (called=%v)", rec.Code, called)
	}
}

func TestMiddlewareStoresUserID(t *testing.T) {
	v := NewVerifier(testSecret)
	var gotID string
	h := v.Middleware(func(w http.ResponseWriter, r *http.Request) {
		gotID, _ = UserIDFromContext(r.Context())
	})

	req := httptest.NewRequest("GET", "/api/connections", nil)
	req.Header.Set("Authorization", "Bearer "+signToken(t, jwt.SigningMethodHS256, testSecret, validClaims()))
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if gotID != "11111111-2222-3333-4444-555555555555" {
		t.Fatalf("user ID not stored in context, got %q", gotID)
	}
}
