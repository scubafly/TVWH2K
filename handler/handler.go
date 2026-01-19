package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"tvwh2k/database"
	"tvwh2k/kraken"
	"tvwh2k/telegram"
)

type WebhookHandler struct {
	krakenClient *kraken.Kraken
	db           *database.DB
}

func NewWebhookHandler(k *kraken.Kraken, db *database.DB) *WebhookHandler {
	return &WebhookHandler{
		krakenClient: k,
		db:           db,
	}
}

type WebhookRequest struct {
	Token     string `json:"token"`
	Text      string `json:"text"`
	Pair      string `json:"pair"`
	Type      string `json:"type"`      // buy/sell
	OrderType string `json:"ordertype"` // market/limit
	Volume    string `json:"volume"`
	Price     string `json:"price"`
	Price2    string `json:"price2"` // Secondary price

	// Conditional Close definitions (e.g. for TP/SL)
	CloseOrderType string `json:"close_ordertype"`
	ClosePrice     string `json:"close_price"`
	ClosePrice2    string `json:"close_price2"`
}

func (h *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req WebhookRequest

	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.Token != os.Getenv("TOKEN") {
		fmt.Printf("Invalid token: %s\n", req.Token)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	fmt.Printf("Received valid webhook for %s %s\n", req.Type, req.Pair)

	// Save signal to DB
	var signalID int64
	if h.db != nil {
		id, err := h.db.SaveSignal(req.Pair, req.Type, req)
		if err != nil {
			fmt.Printf("Failed to save signal: %v\n", err)
		} else {
			signalID = id
		}
	}

	// Send initial notification
	chatIdStr := os.Getenv("TELEGRAM_CHAT_ID")
	chatId, err := strconv.Atoi(chatIdStr)
	if err != nil {
		fmt.Printf("Error parsing chat id: %v\n", err)
	} else {
		msg := fmt.Sprintf("Received Signal: %s", req.Text)
		if req.Pair != "" {
			msg += fmt.Sprintf("\nAction: %s %s %s", req.Type, req.Volume, req.Pair)
		}
		telegram.SendMessage(msg, int64(chatId))
	}

	// Execute Kraken Order if critical fields are present
	if h.krakenClient != nil && req.Pair != "" && req.Type != "" && req.Volume != "" {
		// Default to market if not specified
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

		// Handle Conditional Close (Profit/Stop Loss)
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
			fmt.Println("Attached conditional close order (TP/SL).")
		}

		// Check if we are in test mode via env (or could be in payload)
		if os.Getenv("KRAKEN_TEST_MODE") == "true" {
			orderInput.Validate = true
			fmt.Println("Test mode enabled, validating order only.")
		}

		resp, err := h.krakenClient.AddOrder(orderInput)

		var resultMsg string
		var txid string

		if err != nil {
			resultMsg = fmt.Sprintf("❌ Order Failed: %v", err)
			fmt.Println(resultMsg)
		} else {
			resultMsg = fmt.Sprintf("✅ Order Placed: %s", resp.Description.Order)
			if len(resp.TxID) > 0 {
				txid = resp.TxID[0]
				resultMsg += fmt.Sprintf("\nTxID: %s", txid)
			}
			if resp.Description.Close != "" {
				resultMsg += fmt.Sprintf("\nClose: %s", resp.Description.Close)
			}
			fmt.Println(resultMsg)
		}

		// Save Trade Result to DB
		if h.db != nil && signalID != 0 && txid != "" {
			err := h.db.SaveTrade(signalID, req.Pair, req.Type, req.OrderType, req.Volume, req.Price, txid)
			if err != nil {
				fmt.Printf("Failed to save trade: %v\n", err)
			}
		}

		// Send result to Telegram
		if chatId != 0 {
			telegram.SendMessage(resultMsg, int64(chatId))
		}
	} else if h.krakenClient == nil {
		fmt.Println("Kraken client not initialized, skipping order.")
	}
}

func (h *WebhookHandler) HandleGetSignals(w http.ResponseWriter, r *http.Request) {
	if h.db == nil {
		http.Error(w, "Database not initialized", http.StatusInternalServerError)
		return
	}

	signals, err := h.db.GetRecentSignals(50)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch signals: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(signals)
}

func (h *WebhookHandler) HandleGetTrades(w http.ResponseWriter, r *http.Request) {
	if h.db == nil {
		http.Error(w, "Database not initialized", http.StatusInternalServerError)
		return
	}

	trades, err := h.db.GetRecentTrades(50)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch trades: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(trades)
}
