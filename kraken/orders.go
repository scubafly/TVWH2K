package kraken

import (
	"encoding/json"
	"fmt"
	"net/url"
)

// AddOrder submits a new order to the Kraken API.
func (k *Kraken) AddOrder(order OrderInput) (*AddOrderResponse, error) {
	params := url.Values{}
	params.Set("pair", order.Pair)
	params.Set("type", order.Type)
	params.Set("ordertype", order.OrderType)
	params.Set("volume", order.Volume)

	if order.Price != "" {
		params.Set("price", order.Price)
	}
	if order.Price2 != "" {
		params.Set("price2", order.Price2)
	}
	if order.UserRef != "" {
		params.Set("userref", order.UserRef)
	}
	if order.OFlags != "" {
		params.Set("oflags", order.OFlags)
	}
	if order.TimeInForce != "" {
		params.Set("timeinforce", order.TimeInForce)
	}
	if order.Validate {
		params.Set("validate", "true")
	}

	// Handle conditional close parameters
	// Kraken expects them in the format close[ordertype], close[price], etc.
	if len(order.Close) > 0 {
		for k, v := range order.Close {
			// Construct key like "close[ordertype]"
			key := fmt.Sprintf("close[%s]", k)
			params.Set(key, v)
		}
	}

	// Sign and execute the request
	resp, err := k.doRequest("POST", krakenPrivatePathPrefix+"AddOrder", params)
	if err != nil {
		return nil, err
	}

	// Parse the response
	var genericResp GenericResponse
	if err := json.Unmarshal(resp, &genericResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal AddOrder response: %w", err)
	}

	if len(genericResp.Error) > 0 {
		return nil, fmt.Errorf("Kraken API error: %v", genericResp.Error)
	}

	var addOrderResp AddOrderResponse
	if err := json.Unmarshal(genericResp.Result, &addOrderResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal AddOrder result: %w", err)
	}

	return &addOrderResp, nil
}
