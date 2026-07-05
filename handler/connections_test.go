package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"tvwh2k/auth"
	"tvwh2k/database"
)

func newConnectionsHandlerForTest(t *testing.T, db *database.DB, status string) *ConnectionsHandler {
	t.Helper()
	h := NewConnectionsHandler(db, newTestEncryptor(t), "https://api.example.com", true)
	h.checkCreds = func(apiKey, apiSecret string) string { return status }
	return h
}

func authedRequest(method, target, body string) *http.Request {
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, reader)
	return req.WithContext(auth.ContextWithUserID(req.Context(), "user-1"))
}

func serve(h http.HandlerFunc, pattern string, req *http.Request) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc(pattern, h)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// --- Create ---

func TestCreateRequiresAuth(t *testing.T) {
	db, _ := newMockDB(t)
	h := newConnectionsHandlerForTest(t, db, "ok")
	req := httptest.NewRequest("POST", "/api/connections", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without auth context, got %d", rec.Code)
	}
}

func TestCreateValidatesRequiredFields(t *testing.T) {
	db, _ := newMockDB(t)
	h := newConnectionsHandlerForTest(t, db, "ok")
	rec := httptest.NewRecorder()
	h.Create(rec, authedRequest("POST", "/api/connections", `{"kraken_api_key":""}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing credentials, got %d", rec.Code)
	}
}

func TestCreateStoresConnectionAndReturnsWebhookURL(t *testing.T) {
	db, mock := newMockDB(t)
	h := newConnectionsHandlerForTest(t, db, "ok")

	mock.ExpectQuery("INSERT INTO connections").
		WillReturnRows(connectionRow(t, h.encryptor, 7, "tok123", true))

	rec := httptest.NewRecorder()
	h.Create(rec, authedRequest("POST", "/api/connections", `{"kraken_api_key":"k","kraken_api_secret":"s"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		ID         int64  `json:"id"`
		WebhookURL string `json:"webhook_url"`
		TestMode   bool   `json:"test_mode"`
		Status     string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.WebhookURL != "https://api.example.com/webhooks/tok123" {
		t.Fatalf("wrong webhook URL: %q", resp.WebhookURL)
	}
	if !resp.TestMode || resp.Status != "ok" || resp.ID != 7 {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCreateReportsInvalidCredentials(t *testing.T) {
	db, mock := newMockDB(t)
	h := newConnectionsHandlerForTest(t, db, "invalid_credentials")
	mock.ExpectQuery("INSERT INTO connections").
		WillReturnRows(connectionRow(t, h.encryptor, 8, "tok", true))

	rec := httptest.NewRecorder()
	h.Create(rec, authedRequest("POST", "/api/connections", `{"kraken_api_key":"bad","kraken_api_secret":"bad"}`))

	var resp struct {
		Status string `json:"status"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Status != "invalid_credentials" {
		t.Fatalf("expected invalid_credentials status, got %q", resp.Status)
	}
}

// --- List ---

func TestListReturnsEmptyArrayNotNull(t *testing.T) {
	db, mock := newMockDB(t)
	h := newConnectionsHandlerForTest(t, db, "ok")
	mock.ExpectQuery("SELECT (.+) FROM connections WHERE user_id").
		WithArgs("user-1").
		WillReturnRows(sqlmock.NewRows(connectionCols))

	rec := httptest.NewRecorder()
	h.List(rec, authedRequest("GET", "/api/connections", ""))
	if body := strings.TrimSpace(rec.Body.String()); body != "[]" {
		t.Fatalf("expected [] for empty list, got %q", body)
	}
}

// --- ownership scoping (Test endpoint + signals/trades) ---

func TestTestEndpointRejectsForeignConnection(t *testing.T) {
	db, mock := newMockDB(t)
	h := newConnectionsHandlerForTest(t, db, "ok")
	// DB query is scoped ... AND user_id = $2, so a foreign ID yields no rows.
	mock.ExpectQuery("SELECT (.+) FROM connections WHERE id").
		WithArgs(int64(99), "user-1").
		WillReturnError(sql.ErrNoRows)

	rec := serve(h.Test, "POST /api/connections/{id}/test",
		authedRequest("POST", "/api/connections/99/test", ""))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for connection owned by another user, got %d", rec.Code)
	}
}

func TestTestEndpointRejectsMalformedID(t *testing.T) {
	db, _ := newMockDB(t)
	h := newConnectionsHandlerForTest(t, db, "ok")
	rec := serve(h.Test, "POST /api/connections/{id}/test",
		authedRequest("POST", "/api/connections/12abc/test", ""))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed id, got %d", rec.Code)
	}
}

func TestTestEndpointChecksStoredCredentials(t *testing.T) {
	db, mock := newMockDB(t)
	h := newConnectionsHandlerForTest(t, db, "ok")
	mock.ExpectQuery("SELECT (.+) FROM connections WHERE id").
		WithArgs(int64(7), "user-1").
		WillReturnRows(connectionRow(t, h.encryptor, 7, "tok", true))

	rec := serve(h.Test, "POST /api/connections/{id}/test",
		authedRequest("POST", "/api/connections/7/test", ""))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Fatalf("expected ok status, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- Update (PATCH telegram settings) ---

func TestUpdateSetsTelegramSettings(t *testing.T) {
	db, mock := newMockDB(t)
	h := newConnectionsHandlerForTest(t, db, "ok")

	mock.ExpectQuery("SELECT (.+) FROM connections WHERE id").
		WithArgs(int64(7), "user-1").
		WillReturnRows(connectionRow(t, h.encryptor, 7, "tok", true))

	botEnc, _ := h.encryptor.Encrypt("bot-token")
	updated := sqlmock.NewRows(connectionCols).AddRow(
		int64(7), "user-1", "kraken", []byte("k"), []byte("s"), "tok", true, botEnc, int64(1234), time.Now())
	mock.ExpectQuery("UPDATE connections SET telegram_bot_token_encrypted").
		WillReturnRows(updated)

	rec := serve(h.Update, "PATCH /api/connections/{id}",
		authedRequest("PATCH", "/api/connections/7", `{"telegram_bot_token":"bot-token","telegram_chat_id":1234}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"telegram_enabled":true`) {
		t.Fatalf("expected telegram_enabled true, got %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
