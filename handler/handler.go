package handler

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"tvwh2k/database"
	"tvwh2k/kraken"
	"tvwh2k/telegram"
)

// SpotBalance is the response struct for GET /api/positions.
type SpotBalance struct {
	Asset           string  `json:"asset"`
	Balance         float64 `json:"balance"`
	CurrentPrice    float64 `json:"current_price"`     // EUR price per unit; 0 if pair unknown on Kraken
	CurrentValueEUR float64 `json:"current_value_eur"` // balance × current_price
}

// eurPairForAsset maps a Kraken balance asset name to its EUR ticker pair.
// Kraken's balance API uses internal names with X/Z prefixes (e.g. XXBT, XETH, ZEUR).
// The Ticker API expects the display name without that prefix (e.g. XBTEUR, ETHEUR).
// Returns "" for ZEUR — it is already priced at 1.0 EUR.
func eurPairForAsset(asset string) string {
	if asset == "ZEUR" {
		return ""
	}
	// Strip a single leading X or Z prefix for assets with 4+ chars.
	// XXBT → XBT → XBTEUR, XETH → ETH → ETHEUR, ZUSD → USD → USDEUR
	display := asset
	if len(asset) > 3 && (asset[0] == 'X' || asset[0] == 'Z') {
		display = asset[1:]
	}
	return display + "EUR"
}

type WebhookHandler struct {
	krakenClient *kraken.Kraken
	db           *database.DB
}

func NewWebhookHandler(k *kraken.Kraken, db *database.DB) *WebhookHandler {
	return &WebhookHandler{krakenClient: k, db: db}
}

type WebhookRequest struct {
	Token     string `json:"token"`
	Text      string `json:"text"`
	Pair      string `json:"pair"`
	Type      string `json:"type"`      // "buy" or "sell"
	OrderType string `json:"ordertype"` // "market", "limit", etc.
	Volume    string `json:"volume"`
	Price     string `json:"price"`
	Price2    string `json:"price2"`

	CloseOrderType string `json:"close_ordertype"`
	ClosePrice     string `json:"close_price"`
	ClosePrice2    string `json:"close_price2"`
}

func (h *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req WebhookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.Token != os.Getenv("TOKEN") {
		log.Printf("Invalid webhook token received")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	log.Printf("Received webhook: %s %s", req.Type, req.Pair)

	var signalID int64
	if h.db != nil {
		id, err := h.db.SaveSignal(req.Pair, req.Type, req)
		if err != nil {
			log.Printf("Failed to save signal: %v", err)
		} else {
			signalID = id
		}
	}

	chatIDStr := os.Getenv("TELEGRAM_CHAT_ID")
	chatID, err := strconv.ParseInt(chatIDStr, 10, 64)
	if err != nil && chatIDStr != "" {
		log.Printf("Failed to parse TELEGRAM_CHAT_ID: %v", err)
	}

	if chatID != 0 {
		msg := fmt.Sprintf("Received Signal: %s", req.Text)
		if req.Pair != "" {
			msg += fmt.Sprintf("\nAction: %s %s %s", req.Type, req.Volume, req.Pair)
		}
		telegram.SendMessage(msg, chatID)
	}

	if h.krakenClient == nil {
		log.Println("Kraken client not initialized, skipping order execution.")
		return
	}

	if req.Pair == "" || req.Type == "" || req.Volume == "" {
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
		closeParams := map[string]string{"ordertype": req.CloseOrderType}
		if req.ClosePrice != "" {
			closeParams["price"] = req.ClosePrice
		}
		if req.ClosePrice2 != "" {
			closeParams["price2"] = req.ClosePrice2
		}
		orderInput.Close = closeParams
		log.Println("Attached conditional close order (TP/SL).")
	}

	if os.Getenv("KRAKEN_TEST_MODE") == "true" {
		orderInput.Validate = true
		log.Println("KRAKEN_TEST_MODE: validating order only, not submitting.")
	}

	resp, err := h.krakenClient.AddOrder(orderInput)

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
		log.Println(resultMsg)
	}

	if h.db != nil && signalID != 0 && txid != "" {
		if err := h.db.SaveTrade(signalID, req.Pair, req.Type, req.OrderType, req.Volume, req.Price, txid); err != nil {
			log.Printf("Failed to save trade: %v", err)
		}
	}

	if chatID != 0 {
		telegram.SendMessage(resultMsg, chatID)
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

func (h *WebhookHandler) HandleGetPositions(w http.ResponseWriter, r *http.Request) {
	if h.krakenClient == nil {
		msg := "Kraken client not initialized"
		if os.Getenv("DEBUG_MODE") == "true" || os.Getenv("KRAKEN_TEST_MODE") == "true" {
			var missing []string
			if os.Getenv("KRAKEN_API_KEY") == "" {
				missing = append(missing, "KRAKEN_API_KEY")
			}
			if os.Getenv("KRAKEN_API_SECRET") == "" {
				missing = append(missing, "KRAKEN_API_SECRET")
			}
			if len(missing) > 0 {
				msg = fmt.Sprintf("Kraken client not initialized: missing env vars: %v", missing)
			}
		}
		http.Error(w, msg, http.StatusServiceUnavailable)
		return
	}

	balances, err := h.krakenClient.GetBalance()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch balances: %v", err), http.StatusInternalServerError)
		return
	}

	type holding struct {
		asset   string
		balance float64
	}

	var nonEUR []holding
	eurBalance := 0.0

	for asset, balStr := range *balances {
		bal, err := strconv.ParseFloat(balStr, 64)
		if err != nil || bal <= 0 {
			continue
		}
		if asset == "ZEUR" {
			eurBalance = bal
		} else {
			nonEUR = append(nonEUR, holding{asset, bal})
		}
	}

	// Fetch EUR price per asset individually. Kraken fails the entire bulk
	// ticker call if one pair is unknown, so we call one pair at a time.
	debug := os.Getenv("DEBUG_MODE") == "true"
	assetPrice := make(map[string]float64, len(nonEUR))
	for _, entry := range nonEUR {
		pair := eurPairForAsset(entry.asset)
		if pair == "" {
			continue
		}
		ticker, err := h.krakenClient.GetTicker([]string{pair})
		if err != nil {
			if debug {
				log.Printf("GetTicker: skipping %s (%s): %v", entry.asset, pair, err)
			}
			continue
		}
		// Kraken may return the pair under a slightly different canonical name.
		for _, info := range *ticker {
			if len(info.LastTrade) > 0 {
				if price, err := strconv.ParseFloat(info.LastTrade[0], 64); err == nil {
					assetPrice[entry.asset] = price
				}
			}
			break
		}
	}

	result := make([]SpotBalance, 0, len(nonEUR)+1)

	if eurBalance > 0 {
		result = append(result, SpotBalance{
			Asset:           "ZEUR",
			Balance:         eurBalance,
			CurrentPrice:    1.0,
			CurrentValueEUR: eurBalance,
		})
	}
	for _, entry := range nonEUR {
		price := assetPrice[entry.asset]
		result = append(result, SpotBalance{
			Asset:           entry.asset,
			Balance:         entry.balance,
			CurrentPrice:    price,
			CurrentValueEUR: entry.balance * price,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}
