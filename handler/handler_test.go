package handler

import (
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"tvwh2k/database"
	"tvwh2k/kraken"
	"tvwh2k/security"
)

// --- test fixtures ---

func newTestEncryptor(t *testing.T) *security.Encryptor {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	enc, err := security.NewEncryptor(key)
	if err != nil {
		t.Fatal(err)
	}
	return enc
}

func newMockDB(t *testing.T) (*database.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return &database.DB{DB: db}, mock
}

var connectionCols = []string{
	"id", "user_id", "exchange", "kraken_api_key_encrypted", "kraken_api_secret_encrypted",
	"webhook_token", "test_mode", "telegram_bot_token_encrypted", "telegram_chat_id", "created_at",
}

func connectionRow(t *testing.T, enc *security.Encryptor, id int64, token string, testMode bool) *sqlmock.Rows {
	t.Helper()
	keyEnc, err := enc.Encrypt("api-key")
	if err != nil {
		t.Fatal(err)
	}
	secretEnc, err := enc.Encrypt("api-secret")
	if err != nil {
		t.Fatal(err)
	}
	return sqlmock.NewRows(connectionCols).AddRow(
		id, "user-1", "kraken", keyEnc, secretEnc, token, testMode, nil, nil, time.Now(),
	)
}

// fakeKraken records the order it received and returns a canned response.
type fakeKraken struct {
	gotOrder kraken.OrderInput
	resp     *kraken.AddOrderResponse
	err      error
	done     chan struct{}
}

func (f *fakeKraken) AddOrder(order kraken.OrderInput) (*kraken.AddOrderResponse, error) {
	f.gotOrder = order
	if f.done != nil {
		defer close(f.done)
	}
	return f.resp, f.err
}

func newWebhookHandlerForTest(t *testing.T, db *database.DB, fake *fakeKraken, globalTestMode bool) *WebhookHandler {
	t.Helper()
	h := NewWebhookHandler(db, newTestEncryptor(t), globalTestMode)
	h.newKraken = func(apiKey, apiSecret string) (orderPlacer, error) {
		return fake, nil
	}
	return h
}

func postWebhook(h *WebhookHandler, token, body string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /webhooks/{token}", h.ServeHTTP)
	req := httptest.NewRequest("POST", "/webhooks/"+token, strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

const signalBody = `{"text":"BUY Signal","pair":"XBT/USD","type":"buy","ordertype":"limit","volume":"0.001","price":"95000"}`

// --- webhook endpoint ---

func TestWebhookUnknownTokenReturns401(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery("SELECT (.+) FROM connections WHERE webhook_token").
		WithArgs("nope").WillReturnError(sql.ErrNoRows)

	h := newWebhookHandlerForTest(t, db, &fakeKraken{}, true)
	defer h.Close()

	rec := postWebhook(h, "nope", signalBody)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestWebhookDBErrorReturns503(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery("SELECT (.+) FROM connections WHERE webhook_token").
		WillReturnError(errors.New("connection refused"))

	h := newWebhookHandlerForTest(t, db, &fakeKraken{}, true)
	defer h.Close()

	rec := postWebhook(h, "tok", signalBody)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 on DB error (not 401), got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestWebhookBadJSONReturns400(t *testing.T) {
	db, _ := newMockDB(t)
	h := newWebhookHandlerForTest(t, db, &fakeKraken{}, true)
	defer h.Close()

	rec := postWebhook(h, "tok", "{not json")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestWebhookSignalSaveFailureReturns503(t *testing.T) {
	db, mock := newMockDB(t)
	enc := newTestEncryptor(t)
	mock.ExpectQuery("SELECT (.+) FROM connections WHERE webhook_token").
		WillReturnRows(connectionRow(t, enc, 1, "tok", true))
	mock.ExpectQuery("INSERT INTO signals").
		WillReturnError(errors.New("db down"))

	h := newWebhookHandlerForTest(t, db, &fakeKraken{}, true)
	defer h.Close()

	rec := postWebhook(h, "tok", signalBody)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when signal cannot be persisted, got %d", rec.Code)
	}
}

// mock signal end to end: webhook accepts, worker places (validate-only) order,
// trade row is written with status "validated".
func TestWebhookProcessesMockSignalInTestMode(t *testing.T) {
	db, mock := newMockDB(t)
	enc := newTestEncryptor(t)
	fake := &fakeKraken{
		resp: &kraken.AddOrderResponse{Description: kraken.OrderDescription{Order: "buy 0.001 XBT/USD @ limit 95000"}},
		done: make(chan struct{}),
	}

	mock.ExpectQuery("SELECT (.+) FROM connections WHERE webhook_token").
		WillReturnRows(connectionRow(t, enc, 1, "tok", true))
	mock.ExpectQuery("INSERT INTO signals").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(42))
	mock.ExpectExec("INSERT INTO trades").
		WithArgs(int64(42), int64(1), "XBT/USD", "buy", "limit", "0.001", "95000", "", "validated").
		WillReturnResult(sqlmock.NewResult(1, 1))

	h := NewWebhookHandler(db, enc, true)
	// The connection row above was encrypted with a *different* encryptor than
	// the one inside connectionRow -- use the same enc for both, set here.
	h.newKraken = func(apiKey, apiSecret string) (orderPlacer, error) {
		if apiKey != "api-key" || apiSecret != "api-secret" {
			t.Errorf("decrypted credentials wrong: %q %q", apiKey, apiSecret)
		}
		return fake, nil
	}

	rec := postWebhook(h, "tok", signalBody)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}

	select {
	case <-fake.done:
	case <-time.After(2 * time.Second):
		t.Fatal("worker never placed the order")
	}
	h.Close() // drain so the trade INSERT has happened

	if !fake.gotOrder.Validate {
		t.Fatal("test mode order was not sent with validate=true")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet DB expectations: %v", err)
	}
}

// --- processSignal unit tests ---

func processSignalWith(t *testing.T, fake *fakeKraken, connTestMode, globalTestMode bool, mockSetup func(sqlmock.Sqlmock)) {
	t.Helper()
	db, mock := newMockDB(t)
	enc := newTestEncryptor(t)
	mockSetup(mock)

	h := NewWebhookHandler(db, enc, globalTestMode)
	h.newKraken = func(apiKey, apiSecret string) (orderPlacer, error) { return fake, nil }
	defer h.Close()

	keyEnc, _ := enc.Encrypt("api-key")
	secretEnc, _ := enc.Encrypt("api-secret")
	conn := &database.Connection{ID: 1, KrakenAPIKeyEncrypted: keyEnc, KrakenAPISecretEncrypted: secretEnc, TestMode: connTestMode}

	var req WebhookRequest
	if err := json.Unmarshal([]byte(signalBody), &req); err != nil {
		t.Fatal(err)
	}
	h.processSignal(conn, req, 42)

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet DB expectations: %v", err)
	}
}

func TestProcessSignalLiveModeSavesExecutedTrade(t *testing.T) {
	fake := &fakeKraken{resp: &kraken.AddOrderResponse{
		Description: kraken.OrderDescription{Order: "buy"},
		TxID:        []string{"TX123"},
	}}
	processSignalWith(t, fake, false, false, func(mock sqlmock.Sqlmock) {
		mock.ExpectExec("INSERT INTO trades").
			WithArgs(int64(42), int64(1), "XBT/USD", "buy", "limit", "0.001", "95000", "TX123", "executed").
			WillReturnResult(sqlmock.NewResult(1, 1))
	})
	if fake.gotOrder.Validate {
		t.Fatal("live mode order should not carry validate=true")
	}
}

func TestProcessSignalGlobalTestModeOverridesConnection(t *testing.T) {
	fake := &fakeKraken{resp: &kraken.AddOrderResponse{Description: kraken.OrderDescription{Order: "buy"}}}
	// connection says live, global kill-switch says test -> must validate
	processSignalWith(t, fake, false, true, func(mock sqlmock.Sqlmock) {
		mock.ExpectExec("INSERT INTO trades").
			WithArgs(int64(42), int64(1), "XBT/USD", "buy", "limit", "0.001", "95000", "", "validated").
			WillReturnResult(sqlmock.NewResult(1, 1))
	})
	if !fake.gotOrder.Validate {
		t.Fatal("global test mode did not force validate=true")
	}
}

func TestProcessSignalOrderFailureSavesFailedTrade(t *testing.T) {
	fake := &fakeKraken{err: fmt.Errorf("Kraken API error: [EOrder:Insufficient funds]")}
	processSignalWith(t, fake, false, false, func(mock sqlmock.Sqlmock) {
		mock.ExpectExec("INSERT INTO trades").
			WithArgs(int64(42), int64(1), "XBT/USD", "buy", "limit", "0.001", "95000", "", "failed").
			WillReturnResult(sqlmock.NewResult(1, 1))
	})
}

func TestProcessSignalPanicDoesNotKillWorker(t *testing.T) {
	db, _ := newMockDB(t)
	enc := newTestEncryptor(t)
	h := NewWebhookHandler(db, enc, true)
	h.newKraken = func(apiKey, apiSecret string) (orderPlacer, error) {
		panic("boom")
	}
	keyEnc, _ := enc.Encrypt("k")
	secretEnc, _ := enc.Encrypt("s")
	conn := &database.Connection{ID: 1, KrakenAPIKeyEncrypted: keyEnc, KrakenAPISecretEncrypted: secretEnc}
	h.jobs <- signalJob{Connection: conn, Request: WebhookRequest{Pair: "X", Type: "buy", Volume: "1"}, SignalID: 1}
	done := make(chan struct{})
	go func() { h.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("worker died on panic; Close never returned")
	}
}
