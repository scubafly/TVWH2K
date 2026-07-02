package handler

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"tvwh2k/database"
	"tvwh2k/kraken"
	"tvwh2k/security"
	"tvwh2k/telegram"
)

const (
	workerCount  = 4
	jobQueueSize = 256
)

// WebhookHandler receives TradingView alerts (one per connection, identified
// by the token in the URL) and processes them asynchronously so the webhook
// response isn't held open by Kraken/Telegram network latency.
type WebhookHandler struct {
	db        *database.DB
	encryptor *security.Encryptor
	jobs      chan signalJob
}

type signalJob struct {
	Connection *database.Connection
	Request    WebhookRequest
}

func NewWebhookHandler(db *database.DB, encryptor *security.Encryptor) *WebhookHandler {
	h := &WebhookHandler{
		db:        db,
		encryptor: encryptor,
		jobs:      make(chan signalJob, jobQueueSize),
	}
	for i := 0; i < workerCount; i++ {
		go h.worker()
	}
	return h
}

type WebhookRequest struct {
	Text      string `json:"text"`
	Pair      string `json:"pair"`
	Type      string `json:"type"`      // buy/sell
	OrderType string `json:"ordertype"` // market/limit
	Volume    string `json:"volume"`
	Price     string `json:"price"`
	Price2    string `json:"price2"`

	// Conditional close definitions (e.g. for TP/SL)
	CloseOrderType string `json:"close_ordertype"`
	ClosePrice     string `json:"close_price"`
	ClosePrice2    string `json:"close_price2"`
}

// ServeHTTP handles POST /webhooks/{token}. The token identifies which
// user's connection this signal belongs to -- there's no shared global
// secret anymore, each connection gets its own unguessable URL.
func (h *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if token == "" {
		http.Error(w, "Missing webhook token", http.StatusBadRequest)
		return
	}

	conn, err := h.db.GetConnectionByWebhookToken(token)
	if err != nil {
		http.Error(w, "Unknown webhook token", http.StatusUnauthorized)
		return
	}

	var req WebhookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	select {
	case h.jobs <- signalJob{Connection: conn, Request: req}:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte(`{"status":"accepted"}`))
	default:
		log.Printf("Signal queue full, dropping signal for connection %d", conn.ID)
		http.Error(w, "Server busy, try again", http.StatusServiceUnavailable)
	}
}

func (h *WebhookHandler) worker() {
	for job := range h.jobs {
		h.processSignal(job.Connection, job.Request)
	}
}

// processSignal runs off the hot request path: saves the signal, notifies
// Telegram (if configured for this connection), places the Kraken order
// (or dry-runs it in test mode), and records the resulting trade.
func (h *WebhookHandler) processSignal(conn *database.Connection, req WebhookRequest) {
	fmt.Printf("Processing signal for connection %d: %s %s\n", conn.ID, req.Type, req.Pair)

	signalID, err := h.db.SaveSignal(conn.ID, req.Pair, req.Type, req)
	if err != nil {
		log.Printf("Failed to save signal for connection %d: %v", conn.ID, err)
	}

	notify := func(msg string) {
		if !conn.TelegramChatID.Valid || len(conn.TelegramBotTokenEncrypted) == 0 {
			return
		}
		botToken, err := h.encryptor.Decrypt(conn.TelegramBotTokenEncrypted)
		if err != nil {
			log.Printf("Failed to decrypt telegram bot token for connection %d: %v", conn.ID, err)
			return
		}
		if _, err := telegram.SendMessage(botToken, msg, conn.TelegramChatID.Int64); err != nil {
			log.Printf("Failed to send telegram message for connection %d: %v", conn.ID, err)
		}
	}

	initialMsg := fmt.Sprintf("Received Signal: %s", req.Text)
	if req.Pair != "" {
		initialMsg += fmt.Sprintf("\nAction: %s %s %s", req.Type, req.Volume, req.Pair)
	}
	notify(initialMsg)

	if req.Pair == "" || req.Type == "" || req.Volume == "" {
		return
	}

	apiKey, err := h.encryptor.Decrypt(conn.KrakenAPIKeyEncrypted)
	if err != nil {
		log.Printf("Failed to decrypt Kraken API key for connection %d: %v", conn.ID, err)
		return
	}
	apiSecret, err := h.encryptor.Decrypt(conn.KrakenAPISecretEncrypted)
	if err != nil {
		log.Printf("Failed to decrypt Kraken API secret for connection %d: %v", conn.ID, err)
		return
	}

	krakenClient, err := kraken.NewClient(apiKey, apiSecret)
	if err != nil {
		log.Printf("Failed to build Kraken client for connection %d: %v", conn.ID, err)
		return
	}

	if req.OrderType == "" {
		req.OrderType = "market"
	}

	orderInput := kraken.OrderInput{
		Pair:      req.Pair,
		Type:      req.Type,
		OrderType: req.OrderType,
		Volume:    req.Volume,
		Price:     req.Price,
		Price2:    req.Price2,
	}

	if req.CloseOrderType != "" {
		closeParams := make(map[string]string)
		closeParams["ordertype"] = req.CloseOrderType
		if req.ClosePrice != "" {
			closeParams["price"] = req.ClosePrice
		}
		if req.ClosePrice2 != "" {
			closeParams["price2"] = req.ClosePrice2
		}
		orderInput.Close = closeParams
	}

	if conn.TestMode {
		orderInput.Validate = true
	}

	resp, err := krakenClient.AddOrder(orderInput)

	var resultMsg string
	var txid string

	if err != nil {
		resultMsg = fmt.Sprintf("❌ Order Failed: %v", err)
		log.Println(resultMsg)
	} else {
		resultMsg = fmt.Sprintf("✅ Order Placed: %s", resp.Description.Order)
		if len(resp.TxID) > 0 {
			txid = resp.TxID[0]
			resultMsg += fmt.Sprintf("\nTxID: %s", txid)
		}
		if resp.Description.Close != "" {
			resultMsg += fmt.Sprintf("\nClose: %s", resp.Description.Close)
		}
	}

	if signalID != 0 && txid != "" {
		if err := h.db.SaveTrade(signalID, conn.ID, req.Pair, req.Type, req.OrderType, req.Volume, req.Price, txid); err != nil {
			log.Printf("Failed to save trade for connection %d: %v", conn.ID, err)
		}
	}

	notify(resultMsg)
}

// HandleGetSignals serves GET /api/signals?connection_id=, scoped to the
// authenticated caller.
func (h *WebhookHandler) HandleGetSignals(w http.ResponseWriter, r *http.Request) {
	h.withOwnedConnection(w, r, func(w http.ResponseWriter, conn *database.Connection) {
		signals, err := h.db.GetRecentSignals(conn.ID, 50)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to fetch signals: %v", err), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(signals)
	})
}

// HandleGetTrades serves GET /api/trades?connection_id=, scoped to the
// authenticated caller.
func (h *WebhookHandler) HandleGetTrades(w http.ResponseWriter, r *http.Request) {
	h.withOwnedConnection(w, r, func(w http.ResponseWriter, conn *database.Connection) {
		trades, err := h.db.GetRecentTrades(conn.ID, 50)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to fetch trades: %v", err), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(trades)
	})
}

// withOwnedConnection resolves the ?connection_id= query param and verifies
// it belongs to the JWT-authenticated caller before calling fn.
func (h *WebhookHandler) withOwnedConnection(w http.ResponseWriter, r *http.Request, fn func(http.ResponseWriter, *database.Connection)) {
	userID, ok := authUserID(r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	idStr := r.URL.Query().Get("connection_id")
	if idStr == "" {
		http.Error(w, "Missing connection_id query param", http.StatusBadRequest)
		return
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid connection_id", http.StatusBadRequest)
		return
	}

	conn, err := h.db.GetConnectionByIDForUser(id, userID)
	if err != nil {
		http.Error(w, "Connection not found", http.StatusNotFound)
		return
	}

	fn(w, conn)
}
