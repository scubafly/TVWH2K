package handler

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"tvwh2k/database"
	"tvwh2k/kraken"
	"tvwh2k/security"
)

// ConnectionsHandler lets an authenticated user set up and inspect their own
// TradingView-to-Kraken connection(s).
type ConnectionsHandler struct {
	db         *database.DB
	encryptor  *security.Encryptor
	apiBaseURL string
	testMode   bool // server-wide default, from KRAKEN_TEST_MODE
}

func NewConnectionsHandler(db *database.DB, encryptor *security.Encryptor, apiBaseURL string, testMode bool) *ConnectionsHandler {
	return &ConnectionsHandler{db: db, encryptor: encryptor, apiBaseURL: apiBaseURL, testMode: testMode}
}

type createConnectionRequest struct {
	KrakenAPIKey     string `json:"kraken_api_key"`
	KrakenAPISecret  string `json:"kraken_api_secret"`
	TelegramBotToken string `json:"telegram_bot_token,omitempty"`
	TelegramChatID   int64  `json:"telegram_chat_id,omitempty"`
}

type updateConnectionRequest struct {
	// nil = leave unchanged; empty string / 0 = clear.
	TelegramBotToken *string `json:"telegram_bot_token"`
	TelegramChatID   *int64  `json:"telegram_chat_id"`
}

type connectionResponse struct {
	ID              int64  `json:"id"`
	WebhookURL      string `json:"webhook_url"`
	TestMode        bool   `json:"test_mode"`
	TelegramEnabled bool   `json:"telegram_enabled"`
	CreatedAt       string `json:"created_at"`
}

type createConnectionResponse struct {
	connectionResponse
	Status string `json:"status"` // "ok" | "invalid_credentials", from a live check at creation time
}

func (h *ConnectionsHandler) toResponse(c *database.Connection) connectionResponse {
	return connectionResponse{
		ID:              c.ID,
		WebhookURL:      fmt.Sprintf("%s/webhooks/%s", h.apiBaseURL, c.WebhookToken),
		TestMode:        h.testMode || c.TestMode,
		TelegramEnabled: c.TelegramChatID.Valid && len(c.TelegramBotTokenEncrypted) > 0,
		CreatedAt:       c.CreatedAt.Format(time.RFC3339),
	}
}

// krakenStatus does a live credential check against Kraken's private API.
func krakenStatus(apiKey, apiSecret string) string {
	client, err := kraken.NewClient(apiKey, apiSecret)
	if err != nil {
		return "invalid_credentials"
	}
	if _, err := client.GetBalance(); err != nil {
		return "invalid_credentials"
	}
	return "ok"
}

func generateWebhookToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Create handles POST /api/connections: stores encrypted Kraken credentials,
// tests them once against the live API, and returns the connection's unique
// webhook URL to paste into a TradingView alert. Telegram notification
// settings are optional and can also be set later via PATCH.
func (h *ConnectionsHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID, ok := authUserID(r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req createConnectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.KrakenAPIKey == "" || req.KrakenAPISecret == "" {
		http.Error(w, "kraken_api_key and kraken_api_secret are required", http.StatusBadRequest)
		return
	}

	status := krakenStatus(req.KrakenAPIKey, req.KrakenAPISecret)

	token, err := generateWebhookToken()
	if err != nil {
		http.Error(w, "Failed to generate webhook token", http.StatusInternalServerError)
		return
	}

	keyEnc, err := h.encryptor.Encrypt(req.KrakenAPIKey)
	if err != nil {
		http.Error(w, "Failed to encrypt credentials", http.StatusInternalServerError)
		return
	}
	secretEnc, err := h.encryptor.Encrypt(req.KrakenAPISecret)
	if err != nil {
		http.Error(w, "Failed to encrypt credentials", http.StatusInternalServerError)
		return
	}

	conn, err := h.db.CreateConnection(userID, keyEnc, secretEnc, token, h.testMode)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create connection: %v", err), http.StatusInternalServerError)
		return
	}

	if req.TelegramBotToken != "" && req.TelegramChatID != 0 {
		botTokenEnc, err := h.encryptor.Encrypt(req.TelegramBotToken)
		if err != nil {
			http.Error(w, "Failed to encrypt telegram bot token", http.StatusInternalServerError)
			return
		}
		conn, err = h.db.UpdateConnectionTelegram(conn.ID, userID, botTokenEnc, sql.NullInt64{Int64: req.TelegramChatID, Valid: true})
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to save telegram settings: %v", err), http.StatusInternalServerError)
			return
		}
	}

	writeJSON(w, http.StatusOK, createConnectionResponse{
		connectionResponse: h.toResponse(conn),
		Status:             status,
	})
}

// List handles GET /api/connections. It does not re-check Kraken on every
// call (that would hammer Kraken's rate limits if the frontend polls this) --
// use POST /api/connections/{id}/test for an on-demand re-check.
func (h *ConnectionsHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := authUserID(r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	conns, err := h.db.GetConnectionsByUser(userID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list connections: %v", err), http.StatusInternalServerError)
		return
	}

	out := make([]connectionResponse, 0, len(conns))
	for i := range conns {
		out = append(out, h.toResponse(&conns[i]))
	}
	writeJSON(w, http.StatusOK, out)
}

// ownedConnection resolves the {id} path value and verifies ownership.
func (h *ConnectionsHandler) ownedConnection(w http.ResponseWriter, r *http.Request) (*database.Connection, string, bool) {
	userID, ok := authUserID(r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return nil, "", false
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid connection id", http.StatusBadRequest)
		return nil, "", false
	}

	conn, err := h.db.GetConnectionByIDForUser(id, userID)
	if err != nil {
		http.Error(w, "Connection not found", http.StatusNotFound)
		return nil, "", false
	}
	return conn, userID, true
}

// Update handles PATCH /api/connections/{id}: currently the Telegram
// notification settings (bot token + chat id). Omitted fields are left
// unchanged; an empty bot token or chat id 0 clears the setting.
func (h *ConnectionsHandler) Update(w http.ResponseWriter, r *http.Request) {
	conn, userID, ok := h.ownedConnection(w, r)
	if !ok {
		return
	}

	var req updateConnectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	botTokenEnc := conn.TelegramBotTokenEncrypted
	chatID := conn.TelegramChatID
	if req.TelegramBotToken != nil {
		if *req.TelegramBotToken == "" {
			botTokenEnc = nil
		} else {
			enc, err := h.encryptor.Encrypt(*req.TelegramBotToken)
			if err != nil {
				http.Error(w, "Failed to encrypt telegram bot token", http.StatusInternalServerError)
				return
			}
			botTokenEnc = enc
		}
	}
	if req.TelegramChatID != nil {
		chatID = sql.NullInt64{Int64: *req.TelegramChatID, Valid: *req.TelegramChatID != 0}
	}

	updated, err := h.db.UpdateConnectionTelegram(conn.ID, userID, botTokenEnc, chatID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to update connection: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, h.toResponse(updated))
}

// Test handles POST /api/connections/{id}/test: an on-demand live check of
// the stored Kraken credentials, for a "test connection" button in the UI.
func (h *ConnectionsHandler) Test(w http.ResponseWriter, r *http.Request) {
	conn, _, ok := h.ownedConnection(w, r)
	if !ok {
		return
	}

	apiKey, err := h.encryptor.Decrypt(conn.KrakenAPIKeyEncrypted)
	if err != nil {
		http.Error(w, "Failed to decrypt credentials", http.StatusInternalServerError)
		return
	}
	apiSecret, err := h.encryptor.Decrypt(conn.KrakenAPISecretEncrypted)
	if err != nil {
		http.Error(w, "Failed to decrypt credentials", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": krakenStatus(apiKey, apiSecret)})
}
