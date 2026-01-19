// Package kraken provides a client for interacting with the Kraken REST API.
package kraken

import (
	"encoding/json"
	// No other standard library imports typically needed here,
	// unless you use time.Time and need custom marshalling.
)

// --- Generic Response Handling ---

// GenericResponse represents the basic structure of most Kraken API responses.
// It is used internally to parse the top-level error array before processing the result.
type GenericResponse struct {
	// Error holds a list of error strings. Empty if the request was successful at the API level.
	Error []string `json:"error"`
	// Result holds the specific response data for the successful request.
	// Using json.RawMessage allows delayed parsing into the specific type.
	Result json.RawMessage `json:"result,omitempty"`
}

// --- Order Management Types ---

// OrderInput defines the parameters for placing a new order via the AddOrder method.
// Note: This struct serves to structure input data; it's converted to url.Values,
// so json tags aren't used for submission but are included for clarity/potential other uses.
type OrderInput struct {
	Pair        string `json:"pair"`                  // Asset pair (e.g., "XBT/USD", "ETH/EUR")
	Type        string `json:"type"`                  // Type of order: "buy" or "sell"
	OrderType   string `json:"ordertype"`             // Order type (e.g., "market", "limit", "stop-loss", "take-profit", etc.)
	Volume      string `json:"volume"`                // Order volume in base currency
	Price       string `json:"price,omitempty"`       // Primary price (e.g., limit price) - optional depending on OrderType
	Price2      string `json:"price2,omitempty"`      // Secondary price (e.g., stop loss price) - optional
	UserRef     string `json:"userref,omitempty"`     // Optional user reference ID (should be parseable as int32)
	OFlags      string `json:"oflags,omitempty"`      // Optional comma-delimited list of order flags (e.g., "fcib", "fciq", "nompp", "post")
	TimeInForce string `json:"timeinforce,omitempty"` // Optional time-in-force policy (e.g., "GTC", "IOC", "GTD")
	Validate    bool              `json:"-"`                     // If true, only validate inputs, don't submit. Handled in AddOrder, not sent directly.
	Close       map[string]string `json:"close,omitempty"`       // Conditional close order parameters (e.g. ordertype, price, price2)
	// Add more fields as needed based on Kraken documentation (leverage, starttm, expiretm, etc.)
}

// AddOrderResponse defines the structure of the 'result' field returned by a successful AddOrder call.
type AddOrderResponse struct {
	Description OrderDescription `json:"descr"` // Provides descriptive information about the order.
	TxID        []string         `json:"txid"`  // Array of transaction IDs associated with the order (usually one).
}

// OrderDescription contains descriptive text about the order placed.
type OrderDescription struct {
	Order string `json:"order"`           // Textual description of the order (e.g., "buy 0.1 XBT/USD @ limit 50000").
	Close string `json:"close,omitempty"` // Textual description of the conditional close order (if applicable).
}

// CancelOrderResponse defines the structure of the 'result' field returned by CancelOrder.
type CancelOrderResponse struct {
	Count   int  `json:"count"`   // Number of orders successfully cancelled.
	Pending bool `json:"pending"` // True if cancellation is pending, false otherwise.
}

// --- Account Data Types ---

// BalanceResponse defines the structure of the 'result' field for the GetBalance call.
// It maps asset names (using Kraken's internal naming, e.g., "XXBT", "ZEUR") to balance strings.
type BalanceResponse map[string]string

// TradeBalanceResponse defines the structure of the 'result' field for the GetTradeBalance call.
// All numerical values are returned as strings by the Kraken API to preserve precision.
type TradeBalanceResponse struct {
	EquivalentBalance string `json:"eb,omitempty"` // Combined value of all currencies in base asset.
	TradeBalance      string `json:"tb,omitempty"` // Combined value of equity currencies in base asset.
	MarginAmount      string `json:"m,omitempty"`  // Margin amount of open positions.
	UnrealizedNetPNL  string `json:"n,omitempty"`  // Unrealized net profit/loss of open positions.
	CostBasis         string `json:"c,omitempty"`  // Cost basis of open positions.
	Valuation         string `json:"v,omitempty"`  // Current floating valuation of open positions.
	Equity            string `json:"e,omitempty"`  // Equity = trade balance + unrealized PNL.
	FreeMargin        string `json:"mf,omitempty"` // Free margin = equity - initial margin.
	MarginLevel       string `json:"ml,omitempty"` // Margin level = (equity / initial margin) * 100.
}

// --- Other Common Types ---

// Add structs for other endpoints as needed, for example:
// - OpenOrdersResponse
// - ClosedOrdersResponse
// - TradesHistoryResponse
// - LedgersResponse
// - SystemStatusResponse
// - AssetInfoResponse
// - TradableAssetPairResponse
// etc.

// Example structure for Open Orders (you'd need to define OrderInfo)
/*
type OpenOrdersResponse struct {
	Open map[string]OrderInfo `json:"open"`
}

type OrderInfo struct {
	RefID    string            `json:"refid"` // Referral order transaction ID
	UserRef  int32             `json:"userref"` // User reference ID
	Status   string            `json:"status"`  // Status of order (e.g., "open", "pending")
	OpenTm   float64           `json:"opentm"`  // Unix timestamp of when order was placed
	StartTm  float64           `json:"starttm"` // Unix timestamp of order start time (0 if not set)
	ExpireTm float64           `json:"expiretm"`// Unix timestamp of order end time (0 if not set)
	Descr    OrderDescription  `json:"descr"`   // Order description info
	Vol      string            `json:"vol"`     // Volume of order in base currency
	VolExec  string            `json:"vol_exec"`// Volume executed in base currency
	Cost     string            `json:"cost"`    // Total cost (quote currency)
	Fee      string            `json:"fee"`     // Total fee (quote currency)
	Price    string            `json:"price"`   // Average price executed (quote currency)
	StopPrice string           `json:"stopprice"`// Stop price (quote currency)
	LimitPrice string          `json:"limitprice"`// Triggered limit price (quote currency, for trailing stops)
	Misc     string            `json:"misc"`    // Comma delimited list of miscellaneous info
	OFlags   string            `json:"oflags"`  // Comma delimited list of order flags
	// Potentially add Trades field: []string `json:"trades"` // List of trade IDs related to order
}
*/
