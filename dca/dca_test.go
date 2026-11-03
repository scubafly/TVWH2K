package dca

import (
	"errors"
	"math"
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
				if o.Volume != "0.05882353" { // 50/850
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
	f.orderErr = errors.New("boom")
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
