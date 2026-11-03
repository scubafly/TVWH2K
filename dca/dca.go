// Package dca implements a simple "buy the dip" averaging-down runner for the
// operator's own Kraken SPOT holdings (no margin).
//
// Positions are derived from account balances; the cost basis is the
// volume-weighted average entry from trade history (average-cost method:
// buys add volume and cost incl. fee, sells remove volume at the current
// average cost).
//
// LIMITATIONS
//   - Only EUR-quoted pairs are handled; EUR, stablecoins, staked ("." suffix)
//     assets and dust below DCA_MIN_VALUE_EUR are skipped.
//   - If the balance exceeds the volume derived from trade history (deposits,
//     transfers, airdrops) the basis is unknown and the asset is skipped.
//   - The cooldown map is in-memory and lost on restart.
//   - Overlaps with tribot cmd/portfolio, which already does spot DCA.
package dca

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"tvwh2k/kraken"
)

// Source abstracts Kraken so the logic can be tested without network access.
type Source interface {
	Balances() (map[string]float64, error)
	AssetPairs() (map[string]kraken.AssetPair, error)
	Trades() ([]kraken.Trade, error)
	// TickerPrice returns the last price for a Kraken pair key; it must return
	// an error (never a zero price) when the pair is missing from the ticker.
	TickerPrice(pair string) (float64, error)
	PlaceOrder(o kraken.OrderInput) (*kraken.AddOrderResponse, error)
}

// Config holds the DCA parameters.
type Config struct {
	Threshold   float64       // pnl pct below which a DCA buy triggers (e.g. -10)
	AmountEUR   float64       // quote amount per DCA buy
	Cooldown    time.Duration // minimum time between DCA buys per position
	MinValueEUR float64       // holdings worth less than this are dust and skipped
	TestMode    bool          // true: orders are validate-only
}

// Service runs the DCA logic.
type Service struct {
	src  Source
	cfg  Config
	now  func() time.Time
	mu   sync.Mutex
	last map[string]time.Time // position id -> last successful DCA
}

// New creates a Service.
func New(src Source, cfg Config) *Service {
	return &Service{src: src, cfg: cfg, now: time.Now, last: make(map[string]time.Time)}
}

// Position is a spot holding with its derived cost basis.
type Position struct {
	ID       string // asset/pair
	Asset    string
	Pair     string // Kraken pair key, e.g. XXBTZEUR
	Volume   float64
	AvgEntry float64
}

// PnLPct is the unrealized PnL percentage versus the average entry.
func PnLPct(avgEntry, price float64) float64 { return (price - avgEntry) / avgEntry * 100 }

var stablecoins = map[string]bool{
	"USDT": true, "USDC": true, "DAI": true, "PYUSD": true, "TUSD": true,
	"USDG": true, "EURT": true, "EURR": true, "EURQ": true, "RLUSD": true, "FDUSD": true,
}

// basisTolerance is the relative excess of balance over history-derived volume
// that is still accepted (rounding).
const basisTolerance = 0.01

// AverageCost replays trades (oldest first) for a pair key and returns the
// remaining volume and the volume-weighted average entry. Buy fees are added to
// cost. ok is false if a sell exceeded the held volume (incomplete history).
func AverageCost(trades []kraken.Trade) (vol, avg float64, ok bool, err error) {
	var cost float64
	ok = true
	for _, t := range trades {
		v, e1 := strconv.ParseFloat(t.Vol, 64)
		c, e2 := strconv.ParseFloat(t.Cost, 64)
		f, e3 := strconv.ParseFloat(t.Fee, 64)
		if e1 != nil || e2 != nil || e3 != nil || v <= 0 {
			return 0, 0, false, fmt.Errorf("trade %s: invalid vol/cost/fee", t.ID)
		}
		switch t.Type {
		case "buy":
			vol += v
			cost += c + f
		case "sell":
			if v > vol*(1+1e-9)+1e-12 {
				ok = false
			}
			if vol > 0 {
				cost -= cost / vol * math.Min(v, vol)
			}
			vol = math.Max(vol-v, 0)
			if vol == 0 {
				cost = 0
			}
		}
	}
	if vol > 0 {
		avg = cost / vol
	}
	return vol, avg, ok, nil
}

// eurPairs maps base asset -> EUR-quoted pair key (smallest key wins).
func eurPairs(pairs map[string]kraken.AssetPair) map[string]string {
	out := map[string]string{}
	for key, ap := range pairs {
		if strings.Contains(key, ".") || (ap.Quote != "ZEUR" && ap.Quote != "EUR") {
			continue
		}
		if cur, ok := out[ap.Base]; !ok || key < cur {
			out[ap.Base] = key
		}
	}
	return out
}

// RunOnce evaluates all spot holdings once. Per-position errors are collected
// and returned joined; one failing position does not stop the others.
func (s *Service) RunOnce() error {
	bal, err := s.src.Balances()
	if err != nil {
		return fmt.Errorf("fetch balances: %w", err)
	}
	pairs, err := s.src.AssetPairs()
	if err != nil {
		return fmt.Errorf("fetch asset pairs: %w", err)
	}
	bases := eurPairs(pairs)

	assets := make([]string, 0, len(bal))
	for a := range bal {
		assets = append(assets, a)
	}
	sort.Strings(assets)

	type cand struct {
		asset, pair     string
		vol, price, val float64
	}
	var errs []error
	var cands []cand
	for _, a := range assets {
		vol := bal[a]
		bare := strings.TrimLeft(a, "XZ")
		if vol <= 0 || a == "ZEUR" || a == "EUR" || stablecoins[a] || stablecoins[bare] || strings.Contains(a, ".") {
			continue
		}
		pair, ok := bases[a]
		if !ok {
			log.Printf("DCA: skipping %s, no EUR-quoted pair", a)
			continue
		}
		price, err := s.src.TickerPrice(pair)
		if err != nil {
			e := fmt.Errorf("asset %s pair %s: ticker price: %w", a, pair, err)
			log.Printf("DCA WARNING: %v", e)
			errs = append(errs, e)
			continue
		}
		if vol*price < s.cfg.MinValueEUR {
			continue // dust
		}
		cands = append(cands, cand{a, pair, vol, price, vol * price})
	}

	if len(cands) > 0 {
		trades, err := s.src.Trades()
		if err != nil {
			errs = append(errs, fmt.Errorf("fetch trades history: %w", err))
			return errors.Join(errs...)
		}
		byPair := map[string][]kraken.Trade{}
		for _, t := range trades {
			key, err := kraken.NormalizePair(t.Pair, pairs)
			if err != nil {
				continue // unknown/delisted pair, cannot be one of our candidates
			}
			byPair[key] = append(byPair[key], t)
		}
		for _, c := range cands {
			if err := s.handle(c.asset, c.pair, c.vol, c.price, byPair[c.pair]); err != nil {
				log.Printf("DCA WARNING: %v", err)
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

func (s *Service) handle(asset, pair string, balance, price float64, trades []kraken.Trade) error {
	id := asset + "/" + pair
	vol, avg, ok, err := AverageCost(trades)
	if err != nil {
		return fmt.Errorf("position %s: %w", id, err)
	}
	if !ok || vol <= 0 || balance > vol*(1+basisTolerance) {
		log.Printf("DCA: skipping %s, unknown cost basis (balance %.8f, trade-history volume %.8f)", id, balance, vol)
		return nil
	}
	if avg <= 0 {
		log.Printf("DCA: skipping %s, zero average entry", id)
		return nil
	}
	pnl := PnLPct(avg, price)
	if pnl >= s.cfg.Threshold {
		return nil
	}

	s.mu.Lock()
	last, seen := s.last[id]
	s.mu.Unlock()
	if seen && s.now().Sub(last) < s.cfg.Cooldown {
		return nil
	}

	order := kraken.OrderInput{
		Pair:      pair,
		Type:      "buy",
		OrderType: "market",
		Volume:    strconv.FormatFloat(s.cfg.AmountEUR/price, 'f', 8, 64),
		Validate:  s.cfg.TestMode,
	}
	resp, err := s.src.PlaceOrder(order)
	if err != nil {
		// No cooldown on failure so the next tick retries.
		return fmt.Errorf("position %s: DCA order failed: %w", id, err)
	}
	s.mu.Lock()
	s.last[id] = s.now()
	s.mu.Unlock()
	log.Printf("DCA: position %s avg entry %.4f price %.4f pnl %.2f%% < %.2f%%, placed buy %s (validate=%t): %s",
		id, avg, price, pnl, s.cfg.Threshold, order.Volume, s.cfg.TestMode, resp.Description.Order)
	return nil
}

// Run evaluates on every interval tick until ctx is cancelled.
func (s *Service) Run(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if err := s.RunOnce(); err != nil {
			log.Printf("DCA run finished with errors: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Enabled reports whether DCA_ENABLED is "true" (default false).
func Enabled() bool { return os.Getenv("DCA_ENABLED") == "true" }

// ConfigFromEnv reads DCA_THRESHOLD, DCA_AMOUNT_EUR, DCA_COOLDOWN_HOURS,
// DCA_MIN_VALUE_EUR and KRAKEN_TEST_MODE, plus the run interval from DCA_INTERVAL_MINUTES.
func ConfigFromEnv() (Config, time.Duration, error) {
	threshold, err := envFloat("DCA_THRESHOLD", -10.0)
	if err != nil {
		return Config{}, 0, err
	}
	amount, err := envFloat("DCA_AMOUNT_EUR", 0)
	if err != nil {
		return Config{}, 0, err
	}
	if amount <= 0 {
		return Config{}, 0, fmt.Errorf("DCA_AMOUNT_EUR must be set and > 0")
	}
	hours, err := envFloat("DCA_COOLDOWN_HOURS", 24)
	if err != nil {
		return Config{}, 0, err
	}
	minVal, err := envFloat("DCA_MIN_VALUE_EUR", 5)
	if err != nil {
		return Config{}, 0, err
	}
	mins, err := envFloat("DCA_INTERVAL_MINUTES", 5)
	if err != nil {
		return Config{}, 0, err
	}
	if mins <= 0 || hours < 0 {
		return Config{}, 0, fmt.Errorf("DCA_INTERVAL_MINUTES must be > 0 and DCA_COOLDOWN_HOURS >= 0")
	}
	return Config{
		Threshold:   threshold,
		AmountEUR:   amount,
		Cooldown:    time.Duration(hours * float64(time.Hour)),
		MinValueEUR: minVal,
		TestMode:    os.Getenv("KRAKEN_TEST_MODE") != "false",
	}, time.Duration(mins * float64(time.Minute)), nil
}

func envFloat(name string, def float64) (float64, error) {
	v := os.Getenv(name)
	if v == "" {
		return def, nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: invalid number %q", name, v)
	}
	return f, nil
}

// KrakenSource adapts a *kraken.Kraken to Source. Asset pairs are cached after
// the first successful fetch.
type KrakenSource struct {
	K     *kraken.Kraken
	mu    sync.Mutex
	pairs map[string]kraken.AssetPair
}

func (ks *KrakenSource) Balances() (map[string]float64, error) {
	b, err := ks.K.GetBalance()
	if err != nil {
		return nil, err
	}
	out := make(map[string]float64, len(*b))
	for a, v := range *b {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return nil, fmt.Errorf("balance %s: invalid amount %q", a, v)
		}
		out[a] = f
	}
	return out, nil
}

func (ks *KrakenSource) Trades() ([]kraken.Trade, error) { return ks.K.GetTradesHistory() }

func (ks *KrakenSource) PlaceOrder(o kraken.OrderInput) (*kraken.AddOrderResponse, error) {
	return ks.K.AddOrder(o)
}

func (ks *KrakenSource) AssetPairs() (map[string]kraken.AssetPair, error) {
	ks.mu.Lock()
	defer ks.mu.Unlock()
	if ks.pairs != nil {
		return ks.pairs, nil
	}
	p, err := ks.K.GetAssetPairs()
	if err != nil {
		return nil, err
	}
	ks.pairs = p
	return p, nil
}

func (ks *KrakenSource) TickerPrice(pair string) (float64, error) {
	pairs, err := ks.AssetPairs()
	if err != nil {
		return 0, err
	}
	key, err := kraken.NormalizePair(pair, pairs)
	if err != nil {
		return 0, err
	}
	tk, err := ks.K.GetTicker(key)
	if err != nil {
		return 0, err
	}
	info, ok := tk[key]
	if !ok {
		return 0, fmt.Errorf("pair %q (key %q) missing from ticker response", pair, key)
	}
	return info.LastPrice()
}
