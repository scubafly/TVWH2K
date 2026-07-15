package kraken

import (
	"encoding/json"
	"fmt"
	"net/url"
)

// GetBalance retrieves the account balance for all assets.
// Endpoint: /0/private/Balance
func (k *Kraken) GetBalance() (*BalanceResponse, error) {
	path := krakenPrivatePathPrefix + "Balance"

	respBytes, err := k.doRequest("POST", path, url.Values{})
	if err != nil {
		return nil, fmt.Errorf("GetBalance request failed: %w", err)
	}

	var generic GenericResponse
	if err := json.Unmarshal(respBytes, &generic); err != nil {
		return nil, fmt.Errorf("GetBalance: failed to parse response: %w", err)
	}
	if len(generic.Error) > 0 {
		return nil, fmt.Errorf("GetBalance: API error: %v", generic.Error)
	}
	if generic.Result == nil {
		return nil, fmt.Errorf("GetBalance: response missing result field")
	}

	var result BalanceResponse
	if err := json.Unmarshal(generic.Result, &result); err != nil {
		return nil, fmt.Errorf("GetBalance: failed to parse result: %w", err)
	}

	return &result, nil
}

// GetTradeBalance retrieves trade balance info (equity, free margin, etc.).
// Endpoint: /0/private/TradeBalance
// optionalAsset: calculate balance in this asset (default: ZUSD). Pass "" for default.
func (k *Kraken) GetTradeBalance(optionalAsset string) (*TradeBalanceResponse, error) {
	path := krakenPrivatePathPrefix + "TradeBalance"
	params := url.Values{}
	if optionalAsset != "" {
		params.Set("asset", optionalAsset)
	}

	respBytes, err := k.doRequest("POST", path, params)
	if err != nil {
		return nil, fmt.Errorf("GetTradeBalance request failed: %w", err)
	}

	var generic GenericResponse
	if err := json.Unmarshal(respBytes, &generic); err != nil {
		return nil, fmt.Errorf("GetTradeBalance: failed to parse response: %w", err)
	}
	if len(generic.Error) > 0 {
		return nil, fmt.Errorf("GetTradeBalance: API error: %v", generic.Error)
	}
	if generic.Result == nil {
		return nil, fmt.Errorf("GetTradeBalance: response missing result field")
	}

	var result TradeBalanceResponse
	if err := json.Unmarshal(generic.Result, &result); err != nil {
		return nil, fmt.Errorf("GetTradeBalance: failed to parse result: %w", err)
	}

	return &result, nil
}
