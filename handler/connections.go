package handler

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"

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
}

func NewConnectionsHandler(db *database.DB, encryptor *security.Encryptor, apiBaseURL string) *ConnectionsHandler {
	return &ConnectionsHandler{db: db, encryptor: encryptor, apiBaseURL: apiBaseURL}
}

type createConnectionRequest struct {
	KrakenAPIKey    string `json:"kraken_api_key"`
	KrakenAPISecret string `json:"kraken_api_secret"`
}

type connectionResponse struct {
	ID         int64  `json:"id"`
	WebhookURL string `json:"webhook_url"`
	TestMode   bool   `json:"test_mode"`
	CreatedAt  string `json:"created_at"`
}

type createConnectionResponse struct {
	connectionResponse
	Status string `json:"status"` // "ok" | "invalid_credentials", from a live check at creation time
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
// webhook URL to paste into a TradingView alert.
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

	status := "ok"
	client, err := kraken.NewClient(req.KrakenAPIKey, req.KrakenAPISecret)
	if err != nil {
		status = "invalid_credentials"
	} else if _, err := client.GetBalance(); err != nil {
		status = "invalid_credentials"
	}

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

	conn, err := h.db.CreateConnection(userID, keyEnc, secretEnc, token, true)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create connection: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(createConnectionResponse{
		connectionResponse: connectionResponse{
			ID:         conn.ID,
			WebhookURL: fmt.Sprintf("%s/webhooks/%s", h.apiBaseURL, conn.WebhookToken),
			TestMode:   conn.TestMode,
			CreatedAt:  conn.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		},
		Status: status,
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
	for _, c := range conns {
		out = append(out, connectionResponse{
			ID:         c.ID,
			WebhookURL: fmt.Sprintf("%s/webhooks/%s", h.apiBaseURL, c.WebhookToken),
			TestMode:   c.TestMode,
			CreatedAt:  c.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

// Test handles POST /api/connections/{id}/test: an on-demand live check of
// the stored Kraken credentials, for a "test connection" button in the UI.
func (h *ConnectionsHandler) Test(w http.ResponseWriter, r *http.Request) {
	userID, ok := authUserID(r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	idStr := r.PathValue("id")
	var id int64
	if _, err := fmt.Sscanf(idStr, "%d", &id); err != nil {
		http.Error(w, "Invalid connection id", http.StatusBadRequest)
		return
	}

	conn, err := h.db.GetConnectionByIDForUser(id, userID)
	if err != nil {
		http.Error(w, "Connection not found", http.StatusNotFound)
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

	status := "ok"
	client, err := kraken.NewClient(apiKey, apiSecret)
	if err != nil {
		status = "invalid_credentials"
	} else if _, err := client.GetBalance(); err != nil {
		status = "invalid_credentials"
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": status})
}
