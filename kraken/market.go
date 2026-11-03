package kraken

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// AssetPair is the subset of AssetPairs info needed for pair-name mapping.
type AssetPair struct {
	Altname string `json:"altname"` // e.g. XBTEUR
	Base    string `json:"base"`    // e.g. XXBT
	Quote   string `json:"quote"`   // e.g. ZEUR
	WSName  string `json:"wsname"`  // e.g. XBT/EUR
}

// TickerInfo is the subset of Ticker info we use.
type TickerInfo struct {
	LastTrade []string `json:"c"` // [price, lot volume]
}

// LastPrice returns the last trade price.
func (t TickerInfo) LastPrice() (float64, error) {
	if len(t.LastTrade) == 0 {
		return 0, fmt.Errorf("ticker has no last trade price")
	}
	return strconv.ParseFloat(t.LastTrade[0], 64)
}

// parseResult unmarshals a Kraken envelope, checks errors and decodes result into out.
func parseResult(name string, body []byte, out interface{}) error {
	var g GenericResponse
	if err := json.Unmarshal(body, &g); err != nil {
		return fmt.Errorf("failed to unmarshal %s response: %w", name, err)
	}
	if len(g.Error) > 0 {
		return fmt.Errorf("Kraken API error for %s: %v", name, g.Error)
	}
	if g.Result == nil {
		return fmt.Errorf("Kraken response for %s has no result", name)
	}
	if err := json.Unmarshal(g.Result, out); err != nil {
		return fmt.Errorf("failed to unmarshal %s result: %w", name, err)
	}
	return nil
}

// GetAssetPairs fetches the public AssetPairs, keyed by Kraken's pair key.
func (k *Kraken) GetAssetPairs() (map[string]AssetPair, error) {
	body, err := k.doPublicGet(krakenPublicPathPrefix+"AssetPairs", nil)
	if err != nil {
		return nil, err
	}
	var m map[string]AssetPair
	if err := parseResult("AssetPairs", body, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// GetTicker fetches the public Ticker for the given pairs. The returned map is
// keyed by Kraken's pair key (e.g. XXBTZEUR).
func (k *Kraken) GetTicker(pairs ...string) (map[string]TickerInfo, error) {
	if len(pairs) == 0 {
		return nil, fmt.Errorf("GetTicker: no pairs given")
	}
	params := url.Values{}
	params.Set("pair", strings.Join(pairs, ","))
	body, err := k.doPublicGet(krakenPublicPathPrefix+"Ticker", params)
	if err != nil {
		return nil, err
	}
	var m map[string]TickerInfo
	if err := parseResult("Ticker", body, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// NormalizePair maps a Kraken pair name (internal like XXBTZEUR, short like
// XBTEUR, or wsname like XBT/EUR) to the AssetPairs/Ticker key. It matches the
// key itself, then altname, then wsname (case-insensitive, "/" ignored).
// Returns an error naming the pair if no mapping exists; never guesses.
func NormalizePair(pair string, pairs map[string]AssetPair) (string, error) {
	p := strings.ToUpper(strings.TrimSpace(pair))
	if p == "" {
		return "", fmt.Errorf("empty pair name")
	}
	if _, ok := pairs[p]; ok {
		return p, nil
	}
	flat := strings.ReplaceAll(p, "/", "")
	for key, ap := range pairs {
		if strings.EqualFold(ap.Altname, flat) || strings.EqualFold(strings.ReplaceAll(ap.WSName, "/", ""), flat) {
			return key, nil
		}
	}
	return "", fmt.Errorf("pair %q not found in AssetPairs (no key/altname/wsname match)", pair)
}

// Trade is one executed spot trade from TradesHistory.
type Trade struct {
	ID    string  `json:"-"` // txid (map key in the response)
	Pair  string  `json:"pair"`
	Time  float64 `json:"time"` // unix seconds
	Type  string  `json:"type"` // "buy" or "sell"
	Price string  `json:"price"`
	Cost  string  `json:"cost"` // total quote amount, excluding fee
	Fee   string  `json:"fee"`  // fee in quote currency
	Vol   string  `json:"vol"`  // base volume
}

const maxTradeHistoryPages = 1000

// GetTradesHistory returns all spot trades (private TradesHistory), paginated
// via ofs, sorted oldest first.
func (k *Kraken) GetTradesHistory() ([]Trade, error) {
	var all []Trade
	for page := 0; page < maxTradeHistoryPages; page++ {
		params := url.Values{}
		params.Set("ofs", strconv.Itoa(len(all)))
		body, err := k.doRequest("POST", krakenPrivatePathPrefix+"TradesHistory", params)
		if err != nil {
			return nil, fmt.Errorf("kraken request for TradesHistory failed: %w", err)
		}
		var res struct {
			Trades map[string]Trade `json:"trades"`
			Count  int              `json:"count"`
		}
		if err := parseResult("TradesHistory", body, &res); err != nil {
			return nil, err
		}
		if len(res.Trades) == 0 {
			break
		}
		for id, t := range res.Trades {
			t.ID = id
			all = append(all, t)
		}
		if len(all) >= res.Count {
			break
		}
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].Time != all[j].Time {
			return all[i].Time < all[j].Time
		}
		return all[i].ID < all[j].ID
	})
	return all, nil
}
