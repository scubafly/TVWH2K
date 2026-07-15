package kraken

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// GetTicker retrieves current market prices for the given pairs.
// Endpoint: /0/public/Ticker
// pairs: Kraken pair names, e.g. ["XBTEUR", "ETHEUR"]
//
// Note: Kraken fails the entire request if any single pair is unknown.
// Call this once per pair when you cannot guarantee all pairs are valid.
func (k *Kraken) GetTicker(pairs []string) (*TickerResponse, error) {
	if len(pairs) == 0 {
		return nil, fmt.Errorf("GetTicker: at least one pair is required")
	}

	params := url.Values{}
	params.Set("pair", strings.Join(pairs, ","))

	respBytes, err := k.doPublicGet("/0/public/Ticker", params)
	if err != nil {
		return nil, fmt.Errorf("GetTicker request failed: %w", err)
	}

	var generic GenericResponse
	if err := json.Unmarshal(respBytes, &generic); err != nil {
		return nil, fmt.Errorf("GetTicker: failed to parse response: %w", err)
	}
	if len(generic.Error) > 0 {
		return nil, fmt.Errorf("GetTicker: API error: %v", generic.Error)
	}
	if generic.Result == nil {
		return nil, fmt.Errorf("GetTicker: response missing result field")
	}

	var result TickerResponse
	if err := json.Unmarshal(generic.Result, &result); err != nil {
		return nil, fmt.Errorf("GetTicker: failed to parse result: %w", err)
	}

	return &result, nil
}
