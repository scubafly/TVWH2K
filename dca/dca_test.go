package dca

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tvwh2k/kraken"
)

type fakeSource struct {
	balances map[string]float64
	trades   []kraken.Trade
	prices   map[string]float64
	orderErr error
	orders   []kraken.OrderInput
}

var fakePairs = map[string]kraken.AssetPair{
	"XXBTZEUR": {Altname: "XBTEUR", WSName: "XBT/EUR", Base: "XXBT", Quote: "ZEUR"},
	"XETHZEUR": {Altname: "ETHEUR", WSName: "ETH/EUR", Base: "XETH", Quote: "ZEUR"},
	"XETHZUSD": {Altname: "ETHUSD", WSName: "ETH/USD", Base: "XETH", Quote: "ZUSD"},
	"USDTZEUR": {Altname: "USDTEUR", WSName: "USDT/EUR", Base: "USDT", Quote: "ZEUR"},
}

func (f *fakeSource) Balances() (map[string]float64, error)            { return f.balances, nil }
func (f *fakeSource) AssetPairs() (map[string]kraken.AssetPair, error) { return fakePairs, nil }
func (f *fakeSource) Trades() ([]kraken.Trade, error)                  { return f.trades, nil }
func (f *fakeSource) TickerPrice(pair string) (float64, error) {
	p, ok := f.prices[pair]
	if !ok {
		return 0, errors.New("pair " + pair + " missing from ticker")
	}
	return p, nil
}
func (f *fakeSource) PlaceOrder(o kraken.OrderInput) (*kraken.AddOrderResponse, error) {
	f.orders = append(f.orders, o)
	if f.orderErr != nil {
		return nil, f.orderErr
	}
	return &kraken.AddOrderResponse{}, nil
}

func buy(vol, cost, fee string) kraken.Trade {
	return kraken.Trade{ID: "b", Pair: "XXBTZEUR", Type: "buy", Vol: vol, Cost: cost, Fee: fee}
}
func sell(vol, cost string) kraken.Trade {
	return kraken.Trade{ID: "s", Pair: "XXBTZEUR", Type: "sell", Vol: vol, Cost: cost, Fee: "0"}
}

// 1 XXBT held with average entry 1000: price 1000 is 0%, 850 is -15%.
func btcSource(price float64) *fakeSource {
	return &fakeSource{
		balances: map[string]float64{"XXBT": 1, "ZEUR": 500},
		trades:   []kraken.Trade{buy("1", "1000", "0")},
		prices:   map[string]float64{"XXBTZEUR": price},
	}
}

func newSvc(f *fakeSource, testMode bool) (*Service, *time.Time) {
	s := New(f, Config{Threshold: -10, AmountEUR: 50, Cooldown: 24 * time.Hour, MinValueEUR: 5, TestMode: testMode})
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	return s, &now
}

func TestAverageCost(t *testing.T) {
	tests := []struct {
		name         string
		trades       []kraken.Trade
		wantVol, avg float64
		wantOK       bool
	}{
		{"single buy with fee", []kraken.Trade{buy("2", "2000", "20")}, 2, 1010, true},
		{"two buys", []kraken.Trade{buy("1", "1000", "0"), buy("1", "2000", "0")}, 2, 1500, true},
		{"partial sell keeps avg", []kraken.Trade{buy("1", "1000", "0"), buy("1", "2000", "0"), sell("1", "5000")}, 1, 1500, true},
		{"buy after partial sell", []kraken.Trade{buy("2", "2000", "0"), sell("1", "9000"), buy("1", "3000", "0")}, 2, 2000, true},
		{"full sell resets", []kraken.Trade{buy("1", "1000", "0"), sell("1", "1"), buy("1", "400", "4")}, 1, 404, true},
		{"oversell => incomplete", []kraken.Trade{buy("1", "1000", "0"), sell("2", "1")}, 0, 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			vol, avg, ok, err := AverageCost(tc.trades)
			if err != nil {
				t.Fatal(err)
			}
			if ok != tc.wantOK || math.Abs(vol-tc.wantVol) > 1e-9 || math.Abs(avg-tc.avg) > 1e-9 {
				t.Fatalf("vol=%v avg=%v ok=%v; want %v %v %v", vol, avg, ok, tc.wantVol, tc.avg, tc.wantOK)
			}
		})
	}
	if _, _, _, err := AverageCost([]kraken.Trade{buy("x", "1", "0")}); err == nil {
		t.Error("expected error for invalid trade")
	}
}

func TestUnknownBasisSkipped(t *testing.T) {
	f := btcSource(850)
	f.balances["XXBT"] = 1.5 // 0.5 came from a deposit, not in trade history
	s, _ := newSvc(f, false)
	if err := s.RunOnce(); err != nil || len(f.orders) != 0 {
		t.Fatalf("err=%v orders=%d, want skip", err, len(f.orders))
	}
	f = btcSource(850)
	f.trades = nil // no history at all
	s, _ = newSvc(f, false)
	if err := s.RunOnce(); err != nil || len(f.orders) != 0 {
		t.Fatalf("err=%v orders=%d, want skip", err, len(f.orders))
	}
}

func TestDustStablecoinAndEURSkipped(t *testing.T) {
	f := btcSource(850)
	f.balances = map[string]float64{"XXBT": 0.004, "ZEUR": 900, "USDT": 100, "XETH": 3} // 0.004*850=3.4 < 5
	f.prices["USDTZEUR"] = 0.9
	f.prices["XETHZEUR"] = 1 // would trigger if basis existed; no ETH trades => unknown basis
	s, _ := newSvc(f, false)
	if err := s.RunOnce(); err != nil || len(f.orders) != 0 {
		t.Fatalf("err=%v orders=%d", err, len(f.orders))
	}
}

func TestPartialSellAndFeeDriveTrigger(t *testing.T) {
	f := btcSource(930)
	// avg = (1000+100+... ) : buy 2 for 2000 + fee 40 => 1020; sell 1; 1 left at 1020.
	f.trades = []kraken.Trade{buy("2", "2000", "40"), sell("1", "1500")}
	s, _ := newSvc(f, false)
	if err := s.RunOnce(); err != nil { // 930 vs 1020 = -8.8%
		t.Fatal(err)
	}
	if len(f.orders) != 0 {
		t.Fatalf("orders = %d, want 0 at -8.8%%", len(f.orders))
	}
	f.prices["XXBTZEUR"] = 900 // -11.76%
	if err := s.RunOnce(); err != nil || len(f.orders) != 1 {
		t.Fatalf("err=%v orders=%d", err, len(f.orders))
	}
}

func TestThreshold(t *testing.T) {
	tests := []struct {
		name       string
		price      float64
		wantOrders int
	}{
		{"below threshold", 850, 1},
		{"above threshold", 950, 0},
		{"exactly at threshold", 900, 0},
		{"profit", 1500, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := btcSource(tc.price)
			s, _ := newSvc(f, false)
			if err := s.RunOnce(); err != nil {
				t.Fatal(err)
			}
			if len(f.orders) != tc.wantOrders {
				t.Fatalf("orders = %d, want %d", len(f.orders), tc.wantOrders)
			}
			if tc.wantOrders == 1 {
				o := f.orders[0]
				if o.Type != "buy" || o.Pair != "XXBTZEUR" || o.Validate {
					t.Errorf("unexpected order %+v", o)
				}
				if o.Volume != "0.05882352" { // 50/850
					t.Errorf("volume = %s", o.Volume)
				}
			}
		})
	}
}

func TestCooldownBlocksSecondOrder(t *testing.T) {
	f := btcSource(850)
	s, now := newSvc(f, false)
	s.RunOnce()
	s.RunOnce()
	if len(f.orders) != 1 {
		t.Fatalf("orders = %d, want 1 (cooldown)", len(f.orders))
	}
	*now = now.Add(25 * time.Hour)
	s.RunOnce()
	if len(f.orders) != 2 {
		t.Fatalf("orders = %d, want 2 after cooldown", len(f.orders))
	}
}

func TestFailedOrderDoesNotSetCooldown(t *testing.T) {
	f := btcSource(850)
	f.orderErr = &kraken.APIError{Messages: []string{"EOrder:Insufficient funds"}}
	s, _ := newSvc(f, false)
	if err := s.RunOnce(); err == nil {
		t.Fatal("expected error from failed order")
	}
	f.orderErr = nil
	if err := s.RunOnce(); err != nil {
		t.Fatal(err)
	}
	if len(f.orders) != 2 {
		t.Fatalf("orders = %d, want 2 (retry after failure)", len(f.orders))
	}
}

func TestMissingTickerPairSurfacesError(t *testing.T) {
	f := btcSource(850)
	f.prices = map[string]float64{}
	s, _ := newSvc(f, false)
	err := s.RunOnce()
	if err == nil || !strings.Contains(err.Error(), "XXBTZEUR") {
		t.Fatalf("expected error naming the pair, got %v", err)
	}
	if len(f.orders) != 0 {
		t.Error("no order expected")
	}
}

func TestOneBadPositionDoesNotStopOthers(t *testing.T) {
	f := btcSource(850)
	f.balances["XETH"] = 10 // no ETH ticker => error, BTC must still be processed
	s, _ := newSvc(f, false)
	err := s.RunOnce()
	if err == nil || !strings.Contains(err.Error(), "XETHZEUR") {
		t.Fatalf("expected error naming XETHZEUR, got %v", err)
	}
	if len(f.orders) != 1 {
		t.Fatalf("orders = %d, want 1", len(f.orders))
	}
}

func TestTestModeUsesValidate(t *testing.T) {
	f := btcSource(850)
	s, _ := newSvc(f, true)
	if err := s.RunOnce(); err != nil {
		t.Fatal(err)
	}
	if len(f.orders) != 1 || !f.orders[0].Validate {
		t.Fatalf("expected one validate-only order, got %+v", f.orders)
	}
}

func TestConfigFromEnv(t *testing.T) {
	t.Setenv("DCA_AMOUNT_EUR", "25")
	t.Setenv("KRAKEN_TEST_MODE", "")
	cfg, iv, err := ConfigFromEnv()
	if err != nil || cfg.Threshold != -10 || cfg.Cooldown != 24*time.Hour || iv != 5*time.Minute || !cfg.TestMode || cfg.AmountEUR != 25 {
		t.Fatalf("defaults wrong: %+v %v %v", cfg, iv, err)
	}
	if cfg.MinValueEUR != 5 {
		t.Errorf("MinValueEUR = %v, want 5", cfg.MinValueEUR)
	}
	t.Setenv("DCA_AMOUNT_EUR", "")
	if _, _, err := ConfigFromEnv(); err == nil {
		t.Error("expected error without DCA_AMOUNT_EUR")
	}
}

func TestTransportErrorSetsCooldown(t *testing.T) {
	f := btcSource(850)
	f.orderErr = errors.New("HTTP request execution failed: context deadline exceeded")
	s, _ := newSvc(f, false)
	if err := s.RunOnce(); err == nil {
		t.Fatal("expected error")
	}
	f.orderErr = nil
	s.RunOnce()
	if len(f.orders) != 1 {
		t.Fatalf("orders = %d, want 1 (cooldown after ambiguous failure)", len(f.orders))
	}
	if f.orders[0].UserRef == "" {
		t.Error("userref not set")
	}
}

func TestDefiniteRejectionDoesNotSetCooldown(t *testing.T) {
	for _, msgs := range [][]string{{"EOrder:Insufficient funds"}} {
		f := btcSource(850)
		f.orderErr = fmt.Errorf("wrapped: %w", &kraken.APIError{Messages: msgs})
		s, _ := newSvc(f, false)
		s.RunOnce()
		f.orderErr = nil
		s.RunOnce()
		if len(f.orders) != 2 {
			t.Fatalf("%v: orders = %d, want 2 (retry after rejection)", msgs, len(f.orders))
		}
	}
	// Ambiguous EService errors are not definite.
	f := btcSource(850)
	f.orderErr = &kraken.APIError{Messages: []string{"EService:Busy"}}
	s, _ := newSvc(f, false)
	s.RunOnce()
	f.orderErr = nil
	s.RunOnce()
	if len(f.orders) != 1 {
		t.Fatalf("EService: orders = %d, want 1", len(f.orders))
	}
}

func TestDefiniteRejectionAllowlist(t *testing.T) {
	cases := map[string]bool{
		"EGeneral:Invalid arguments": true, "EGeneral:Permission denied": true,
		"EOrder:Insufficient funds": true, "EOrder:Order minimum not met": true,
		"EFunding:Unknown withdraw key": true, "EAPI:Invalid key": true,
		"EAPI:Invalid signature": true, "EAPI:Invalid nonce": true,
		"EGeneral:Internal error": false, "EService:Busy": false,
		"EService:Unavailable": false, "EWhatever:new": false,
	}
	for m, want := range cases {
		if got := definiteRejection(&kraken.APIError{Messages: []string{m}}); got != want {
			t.Errorf("%s: got %v want %v", m, got, want)
		}
	}
	if definiteRejection(&kraken.APIError{Messages: []string{"EOrder:x", "EService:Busy"}}) {
		t.Error("mixed with ambiguous must not be definite")
	}
}

func TestLiveRequiresStateFile(t *testing.T) {
	t.Setenv("DCA_AMOUNT_EUR", "50")
	t.Setenv("KRAKEN_TEST_MODE", "false")
	t.Setenv("DCA_LIVE", "true")
	t.Setenv("DCA_STATE_FILE", "")
	if _, _, err := ConfigFromEnv(); err == nil {
		t.Error("live without DCA_STATE_FILE must fail")
	}
	t.Setenv("DCA_STATE_FILE", "/tmp/x.json")
	if _, _, err := ConfigFromEnv(); err != nil {
		t.Errorf("live with state file: %v", err)
	}
	t.Setenv("DCA_LIVE", "false")
	t.Setenv("DCA_STATE_FILE", "")
	if _, _, err := ConfigFromEnv(); err != nil {
		t.Errorf("validate mode without state file: %v", err)
	}
}

func TestCorruptStateFileStartupError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	os.WriteFile(path, []byte("{nope"), 0o600)
	if New(&fakeSource{}, Config{StateFile: path}).StartupError() == nil {
		t.Error("expected StartupError for corrupt state")
	}
	if New(&fakeSource{}, Config{StateFile: path + ".missing"}).StartupError() != nil {
		t.Error("missing file is not an error")
	}
}

func TestVolumeRoundedDownToLotDecimals(t *testing.T) {
	f := btcSource(850) // 50/850 = 0.0588235294...
	three := 3
	old := fakePairs["XXBTZEUR"]
	p := old
	p.LotDecimals = &three
	fakePairs["XXBTZEUR"] = p
	defer func() { fakePairs["XXBTZEUR"] = old }()
	s, _ := newSvc(f, false)
	s.RunOnce()
	if len(f.orders) != 1 || f.orders[0].Volume != "0.058" {
		t.Fatalf("orders = %+v, want volume 0.058", f.orders)
	}
}

func TestDailyCapBlocks(t *testing.T) {
	f := btcSource(850)
	f.balances["XETH"] = 1
	f.prices["XETHZEUR"] = 850
	f.trades = append(f.trades, kraken.Trade{ID: "e", Pair: "XETHZEUR", Type: "buy", Vol: "1", Cost: "1000", Fee: "0"})
	s, now := newSvc(f, false)
	s.cfg.MaxDailyEUR = 50 // AmountEUR is 50: only one order per day
	s.RunOnce()
	if len(f.orders) != 1 {
		t.Fatalf("orders = %d, want 1 (cap)", len(f.orders))
	}
	*now = now.Add(25 * time.Hour) // next UTC day, cooldown over
	s.RunOnce()
	if len(f.orders) != 2 {
		t.Fatalf("orders = %d, want 2 on next day", len(f.orders))
	}
}

func TestStateFileRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	f := btcSource(850)
	s, now := newSvc(f, false)
	s.cfg.StateFile = path
	s.cfg.MaxDailyEUR = 50
	s.RunOnce()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 600", fi.Mode().Perm())
	}
	f2 := btcSource(850)
	s2 := New(f2, Config{Threshold: -10, AmountEUR: 50, Cooldown: 24 * time.Hour, MinValueEUR: 5, MaxDailyEUR: 50, StateFile: path})
	s2.now = func() time.Time { return now.Add(time.Hour) }
	s2.RunOnce()
	if len(f2.orders) != 0 {
		t.Fatalf("orders after restart = %d, want 0 (state restored)", len(f2.orders))
	}
	// corrupt file is ignored
	os.WriteFile(path, []byte("{nope"), 0o600)
	New(f2, Config{StateFile: path})
}

func TestConfigDailyDefault(t *testing.T) {
	t.Setenv("DCA_AMOUNT_EUR", "25")
	cfg, _, err := ConfigFromEnv()
	if err != nil || cfg.MaxDailyEUR != 75 {
		t.Fatalf("MaxDailyEUR = %v err %v, want 75", cfg.MaxDailyEUR, err)
	}
	t.Setenv("DCA_MAX_DAILY_EUR", "0")
	cfg, _, _ = ConfigFromEnv()
	if cfg.MaxDailyEUR != 0 {
		t.Errorf("0 should disable, got %v", cfg.MaxDailyEUR)
	}
}

func TestConfigThresholdMustBeNegative(t *testing.T) {
	t.Setenv("DCA_AMOUNT_EUR", "25")
	for _, v := range []string{"0", "5"} {
		t.Setenv("DCA_THRESHOLD", v)
		if _, _, err := ConfigFromEnv(); err == nil {
			t.Errorf("threshold %s should be refused", v)
		}
	}
	t.Setenv("DCA_THRESHOLD", "-5")
	if _, _, err := ConfigFromEnv(); err != nil {
		t.Error(err)
	}
}

func TestConfigLiveNeedsBothSwitches(t *testing.T) {
	t.Setenv("DCA_AMOUNT_EUR", "25")
	t.Setenv("DCA_STATE_FILE", "/tmp/dca-state.json")
	for _, tc := range []struct {
		km, live string
		wantTest bool
	}{
		{"false", "true", false},
		{"false", "", true},
		{"false", "false", true},
		{"true", "true", true},
		{"", "true", true},
	} {
		t.Setenv("KRAKEN_TEST_MODE", tc.km)
		t.Setenv("DCA_LIVE", tc.live)
		cfg, _, err := ConfigFromEnv()
		if err != nil || cfg.TestMode != tc.wantTest {
			t.Errorf("KRAKEN_TEST_MODE=%q DCA_LIVE=%q: TestMode=%v want %v (%v)", tc.km, tc.live, cfg.TestMode, tc.wantTest, err)
		}
	}
}

func TestMarginTradesSkippedInAverageCost(t *testing.T) {
	m := buy("1", "5000", "0")
	m.PosStatus = "open"
	vol, avg, ok, err := AverageCost([]kraken.Trade{buy("1", "1000", "0"), m})
	if err != nil || !ok || vol != 1 || avg != 1000 {
		t.Fatalf("vol=%v avg=%v ok=%v err=%v", vol, avg, ok, err)
	}
	m2 := buy("1", "5000", "0")
	m2.Margin = "12.5"
	if vol, _, _, _ := AverageCost([]kraken.Trade{m2}); vol != 0 {
		t.Errorf("margin amount trade counted, vol=%v", vol)
	}
}

func TestRunWaitsOneIntervalWithoutState(t *testing.T) {
	f := btcSource(850)
	s, _ := newSvc(f, false)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx, time.Hour); close(done) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done
	if len(f.orders) != 0 {
		t.Fatalf("orders = %d, want 0 (no immediate run without state)", len(f.orders))
	}

	// With a loaded state file the first run is immediate.
	path := filepath.Join(t.TempDir(), "s.json")
	os.WriteFile(path, []byte(`{"last":{},"spent_day":"","spent":0}`), 0o600)
	f2 := btcSource(850)
	s2 := New(f2, Config{Threshold: -10, AmountEUR: 50, Cooldown: time.Hour, MinValueEUR: 5, StateFile: path})
	ctx2, cancel2 := context.WithCancel(context.Background())
	done2 := make(chan struct{})
	go func() { s2.Run(ctx2, time.Hour); close(done2) }()
	time.Sleep(100 * time.Millisecond)
	cancel2()
	<-done2
	if len(f2.orders) != 1 {
		t.Fatalf("orders = %d, want 1 (immediate run with state)", len(f2.orders))
	}
}
