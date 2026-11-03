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
//   - The cooldown map and daily spend are in-memory unless DCA_STATE_FILE is set.
//   - An order whose outcome is unknown (transport error/timeout, ambiguous
//     EService error) is treated as placed: cooldown and daily spend are
//     recorded so no duplicate live order follows; check Kraken manually.
//   - Overlaps with tribot cmd/portfolio, which already does spot DCA.
package dca

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"log"
	"math"
	"os"
	"path/filepath"
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
	MaxDailyEUR float64       // max quote amount placed per UTC day; 0 disables
	StateFile   string        // optional JSON file persisting cooldown + daily spend; "" = memory only
}

// Service runs the DCA logic.
type Service struct {
	src  Source
	cfg  Config
	now  func() time.Time
	mu   sync.Mutex
	last map[string]time.Time // position id -> last (possibly) placed DCA

	stateLoaded bool  // a state file existed and parsed at startup
	loadErr     error // state file existed but could not be read/parsed

	spentDay string  // UTC day (2006-01-02) that spent refers to
	spent    float64 // quote amount placed on spentDay
}

// New creates a Service. If cfg.StateFile is set and exists it is loaded; a
// corrupt file is logged and ignored (starts empty); callers that must not
// run without state check StartupError.
func New(src Source, cfg Config) *Service {
	s := &Service{src: src, cfg: cfg, now: time.Now, last: make(map[string]time.Time)}
	if cfg.StateFile != "" {
		if err := s.loadState(); err != nil {
			log.Printf("DCA WARNING: cannot load state file %s: %v", cfg.StateFile, err)
			s.loadErr = err
		}
	}
	return s
}

// StartupError is non-nil when the state file exists but is corrupt/unreadable.
// In live mode the caller must refuse to start: cooldown and daily spend are
// unknown and starting empty could duplicate buys.
func (s *Service) StartupError() error { return s.loadErr }

type stateFile struct {
	Last     map[string]time.Time `json:"last"`
	SpentDay string               `json:"spent_day"`
	Spent    float64              `json:"spent"`
}

func (s *Service) loadState() error {
	b, err := os.ReadFile(s.cfg.StateFile)
	if errors.Is(err, os.ErrNotExist) {
		if !s.cfg.TestMode {
			log.Printf("DCA WARNING: state file missing: cooldown and daily spend start empty (%s)", s.cfg.StateFile)
		}
		return nil // first start: nothing loaded, Run waits one interval
	}
	if err != nil {
		return err
	}
	var st stateFile
	if err := json.Unmarshal(b, &st); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range st.Last {
		s.last[k] = v
	}
	s.spentDay, s.spent = st.SpentDay, st.Spent
	s.stateLoaded = true
	return nil
}

// saveState writes the state atomically (temp file + rename) with mode 600.
// Caller must not hold s.mu.
func (s *Service) saveState() error {
	if s.cfg.StateFile == "" {
		return nil
	}
	s.mu.Lock()
	st := stateFile{Last: make(map[string]time.Time, len(s.last)), SpentDay: s.spentDay, Spent: s.spent}
	for k, v := range s.last {
		st.Last[k] = v
	}
	s.mu.Unlock()
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.cfg.StateFile), ".dca-state-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after successful rename
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.cfg.StateFile)
}

// spentToday returns the amount placed on the current UTC day. Caller holds s.mu.
func (s *Service) spentToday() float64 {
	if day := s.now().UTC().Format("2006-01-02"); day != s.spentDay {
		s.spentDay, s.spent = day, 0
	}
	return s.spent
}

// definiteRejection reports whether err is a clear Kraken API rejection, i.e.
// the order certainly was not placed. Only an allowlist of validation/permission
// errors counts; transport errors, timeouts, non-2xx responses, unparsable
// bodies, EGeneral:Internal error, EService:* and unknown errors may have left
// a live order and are NOT definite.
func definiteRejection(err error) bool {
	var ae *kraken.APIError
	if !errors.As(err, &ae) || len(ae.Messages) == 0 {
		return false
	}
	for _, m := range ae.Messages {
		if !definiteMessage(m) {
			return false
		}
	}
	return true
}

func definiteMessage(m string) bool {
	switch {
	case m == "EGeneral:Invalid arguments", m == "EGeneral:Permission denied",
		strings.HasPrefix(m, "EGeneral:Invalid arguments:"),
		strings.HasPrefix(m, "EOrder:"), strings.HasPrefix(m, "EFunding:"),
		m == "EAPI:Invalid key", m == "EAPI:Invalid signature", m == "EAPI:Invalid nonce":
		return true
	}
	return false
}

// userRef derives a deterministic positive int32 reference for a position and
// UTC day, so a possible duplicate can be found in Kraken's order list.
func userRef(id string, now time.Time) string {
	h := fnv.New32a()
	h.Write([]byte(id + "|" + now.UTC().Format("2006-01-02")))
	return strconv.FormatInt(int64(h.Sum32()&0x7fffffff), 10)
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
		if t.IsMargin() {
			continue // margin trades are not spot holdings
		}
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
		lotDec          int
		orderMin        string
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
		cands = append(cands, cand{a, pair, vol, price, vol * price, lotDecimals(pairs[pair]), pairs[pair].OrderMin})
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
			if err := s.handle(c.asset, c.pair, c.vol, c.price, c.lotDec, c.orderMin, byPair[c.pair]); err != nil {
				log.Printf("DCA WARNING: %v", err)
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

func (s *Service) handle(asset, pair string, balance, price float64, lotDec int, orderMin string, trades []kraken.Trade) error {
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

	s.mu.Lock()
	over := s.cfg.MaxDailyEUR > 0 && s.spentToday()+s.cfg.AmountEUR > s.cfg.MaxDailyEUR
	s.mu.Unlock()
	if over {
		log.Printf("DCA: skipping %s, daily cap DCA_MAX_DAILY_EUR %.2f reached", id, s.cfg.MaxDailyEUR)
		return nil
	}

	orderVol := roundDown(s.cfg.AmountEUR/price, lotDec)
	if orderVol <= 0 {
		log.Printf("DCA: skipping %s, amount %.2f buys zero volume at price %.4f (lot decimals %d)", id, s.cfg.AmountEUR, price, lotDec)
		return nil
	}

	if minVol, err := strconv.ParseFloat(orderMin, 64); err == nil && minVol > 0 && orderVol < minVol {
		log.Printf("DCA: skipping %s, volume %s below pair ordermin %s", id, strconv.FormatFloat(orderVol, 'f', lotDec, 64), orderMin)
		return nil
	}

	order := kraken.OrderInput{
		UserRef:   userRef(id, s.now()),
		Pair:      pair,
		Type:      "buy",
		OrderType: "market",
		Volume:    strconv.FormatFloat(orderVol, 'f', lotDec, 64),
		Validate:  s.cfg.TestMode,
	}
	resp, err := s.src.PlaceOrder(order)
	if err != nil && definiteRejection(err) {
		// Kraken clearly refused: nothing was placed, so the next tick may retry.
		return fmt.Errorf("position %s: DCA order rejected: %w", id, err)
	}
	// Success or unknown outcome (transport error/timeout may have reached
	// Kraken): treat as placed to avoid a duplicate live order.
	s.mu.Lock()
	s.last[id] = s.now()
	s.spentToday()
	s.spent += s.cfg.AmountEUR
	s.mu.Unlock()
	if serr := s.saveState(); serr != nil {
		log.Printf("DCA WARNING: cannot save state: %v", serr)
	}
	if err != nil {
		return fmt.Errorf("position %s: DCA order outcome unknown, cooldown set, verify on Kraken (userref %s): %w", id, order.UserRef, err)
	}
	log.Printf("DCA: position %s avg entry %.4f price %.4f pnl %.2f%% < %.2f%%, placed buy %s (validate=%t): %s",
		id, avg, price, pnl, s.cfg.Threshold, order.Volume, s.cfg.TestMode, resp.Description.Order)
	return nil
}

// lotDecimals is the pair's volume precision; 8 when Kraken did not report it.
func lotDecimals(p kraken.AssetPair) int {
	if p.LotDecimals == nil {
		return 8
	}
	return *p.LotDecimals
}

// roundDown truncates v to dec decimals (never rounds up, so the order never
// exceeds the intended quote amount).
func roundDown(v float64, dec int) float64 {
	m := math.Pow(10, float64(dec))
	return math.Floor(v*m+1e-9) / m
}

// Run evaluates on every interval tick until ctx is cancelled.
func (s *Service) Run(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	// Without a successfully loaded state file the cooldown/daily spend are
	// unknown after a (re)start, so wait one interval before the first run.
	s.mu.Lock()
	immediate := s.stateLoaded
	s.mu.Unlock()
	if !immediate {
		log.Printf("DCA: no state loaded, first run after %s", interval)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
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
// DCA_MIN_VALUE_EUR, DCA_MAX_DAILY_EUR (default 3x DCA_AMOUNT_EUR, 0 disables),
// DCA_STATE_FILE, KRAKEN_TEST_MODE and DCA_LIVE, plus the run interval from DCA_INTERVAL_MINUTES.
func ConfigFromEnv() (Config, time.Duration, error) {
	threshold, err := envFloat("DCA_THRESHOLD", -10.0)
	if err != nil {
		return Config{}, 0, err
	}
	if threshold >= 0 {
		return Config{}, 0, fmt.Errorf("DCA_THRESHOLD must be < 0 (got %v): a non-negative threshold would buy on gains", threshold)
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
	maxDaily, err := envFloat("DCA_MAX_DAILY_EUR", amount*3)
	if err != nil {
		return Config{}, 0, err
	}
	if maxDaily < 0 {
		return Config{}, 0, fmt.Errorf("DCA_MAX_DAILY_EUR must be >= 0")
	}
	mins, err := envFloat("DCA_INTERVAL_MINUTES", 5)
	if err != nil {
		return Config{}, 0, err
	}
	testMode := !(os.Getenv("KRAKEN_TEST_MODE") == "false" && os.Getenv("DCA_LIVE") == "true")
	if !testMode && os.Getenv("DCA_STATE_FILE") == "" {
		return Config{}, 0, fmt.Errorf("DCA_STATE_FILE is required when DCA is live (KRAKEN_TEST_MODE=false and DCA_LIVE=true)")
	}
	if mins <= 0 || hours < 0 {
		return Config{}, 0, fmt.Errorf("DCA_INTERVAL_MINUTES must be > 0 and DCA_COOLDOWN_HOURS >= 0")
	}
	return Config{
		Threshold:   threshold,
		AmountEUR:   amount,
		Cooldown:    time.Duration(hours * float64(time.Hour)),
		MinValueEUR: minVal,
		MaxDailyEUR: maxDaily,
		StateFile:   os.Getenv("DCA_STATE_FILE"),
		// Live orders need BOTH switches; anything else is validate-only.
		TestMode: testMode,
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
