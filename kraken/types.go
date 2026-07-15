package kraken

import "encoding/json"

// GenericResponse is the top-level structure of all Kraken API responses.
type GenericResponse struct {
	Error  []string        `json:"error"`
	Result json.RawMessage `json:"result,omitempty"`
}

// --- Order types ---

// OrderInput defines the parameters for placing an order via AddOrder.
type OrderInput struct {
	Pair        string            `json:"pair"`
	Type        string            `json:"type"`                  // "buy" or "sell"
	OrderType   string            `json:"ordertype"`             // "market", "limit", "stop-loss", etc.
	Volume      string            `json:"volume"`                // Base currency volume
	Price       string            `json:"price,omitempty"`       // Primary price (limit, stop)
	Price2      string            `json:"price2,omitempty"`      // Secondary price
	UserRef     string            `json:"userref,omitempty"`
	OFlags      string            `json:"oflags,omitempty"`
	TimeInForce string            `json:"timeinforce,omitempty"`
	Validate    bool              `json:"-"`                     // If true, validate only — do not submit.
	Close       map[string]string `json:"close,omitempty"`       // Conditional close (TP/SL) parameters
}

// AddOrderResponse is the result of a successful AddOrder call.
type AddOrderResponse struct {
	Description OrderDescription `json:"descr"`
	TxID        []string         `json:"txid"`
}

// OrderDescription contains the human-readable order summary returned by Kraken.
type OrderDescription struct {
	Order string `json:"order"`
	Close string `json:"close,omitempty"`
}

// CancelOrderResponse is the result of a CancelOrder call.
type CancelOrderResponse struct {
	Count   int  `json:"count"`
	Pending bool `json:"pending"`
}

// --- Account types ---

// BalanceResponse maps Kraken's internal asset names to their balance strings.
// Keys use Kraken's internal notation, e.g. "XXBT", "XETH", "ZEUR".
type BalanceResponse map[string]string

// TradeBalanceResponse contains margin account metrics returned by TradeBalance.
// All numeric values are returned as strings by Kraken to preserve precision.
type TradeBalanceResponse struct {
	EquivalentBalance string `json:"eb,omitempty"`
	TradeBalance      string `json:"tb,omitempty"`
	MarginAmount      string `json:"m,omitempty"`
	UnrealizedNetPNL  string `json:"n,omitempty"`
	CostBasis         string `json:"c,omitempty"`
	Valuation         string `json:"v,omitempty"`
	Equity            string `json:"e,omitempty"`
	FreeMargin        string `json:"mf,omitempty"`
	MarginLevel       string `json:"ml,omitempty"`
}

// --- Ticker types ---

// TickerInfo holds market data for a single asset pair.
type TickerInfo struct {
	Ask       []string `json:"a"` // [price, whole lot volume, lot volume]
	Bid       []string `json:"b"` // [price, whole lot volume, lot volume]
	LastTrade []string `json:"c"` // [price, lot volume]
	Volume    []string `json:"v"` // [today, last 24h]
	VWAP      []string `json:"p"` // [today, last 24h]
	NumTrades []int    `json:"t"` // [today, last 24h]
	Low       []string `json:"l"` // [today, last 24h]
	High      []string `json:"h"` // [today, last 24h]
	OpenPrice string   `json:"o"` // Today's opening price
}

// TickerResponse maps pair names to their TickerInfo.
type TickerResponse map[string]TickerInfo
