package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

func TestRateLimiterTokenBucket(t *testing.T) {
	l := newRateLimiter(60, 2) // 1/sec, burst 2
	t0 := time.Unix(1000, 0)
	steps := []struct {
		at   time.Duration
		key  string
		want bool
	}{
		{0, "a", true}, {0, "a", true}, {0, "a", false},
		{0, "b", true},                       // separate key
		{500 * time.Millisecond, "a", false}, // not refilled yet
		{1100 * time.Millisecond, "a", true}, // refilled
	}
	for i, s := range steps {
		if got, _ := l.allow(s.key, t0.Add(s.at)); got != s.want {
			t.Fatalf("step %d: got %v want %v", i, got, s.want)
		}
	}
}

func TestDeduper(t *testing.T) {
	d := newDeduper(time.Minute)
	t0 := time.Unix(1000, 0)
	if !d.firstSeen("k", t0) {
		t.Fatal("first should pass")
	}
	if d.firstSeen("k", t0.Add(30*time.Second)) {
		t.Fatal("repeat in window should be duplicate")
	}
	if !d.firstSeen("k", t0.Add(61*time.Second)) {
		t.Fatal("after window should pass")
	}
	d.forget("k")
	if !d.firstSeen("k", t0.Add(62*time.Second)) {
		t.Fatal("after forget should pass")
	}
	if !newDeduper(0).firstSeen("k", t0) || !newDeduper(0).firstSeen("k", t0) {
		t.Fatal("window 0 disables dedup")
	}
}

func TestDedupKey(t *testing.T) {
	a := WebhookRequest{Pair: "XBT/USD", Type: "buy", Volume: "1"}
	b := a
	b.IdempotencyID = "x1"
	c := a
	c.Text = "other"
	d := b
	d.Text = "different text, same id"
	if dedupKey("t", a) != dedupKey("t", a) {
		t.Error("same payload must match")
	}
	if dedupKey("t", a) == dedupKey("t", c) {
		t.Error("different payload must differ")
	}
	if dedupKey("t", a) == dedupKey("u", a) {
		t.Error("different token must differ")
	}
	if dedupKey("t", b) != dedupKey("t", d) {
		t.Error("same idempotency id must match regardless of payload")
	}
}

func TestClientIP(t *testing.T) {
	r := httptest.NewRequest("POST", "/", nil)
	r.RemoteAddr = "1.2.3.4:5555"
	r.Header.Set("X-Forwarded-For", "9.9.9.9, 5.6.7.8")
	if got := clientIP(r, false); got != "1.2.3.4" {
		t.Errorf("untrusted: %s", got)
	}
	if got := clientIP(r, true); got != "5.6.7.8" {
		t.Errorf("trusted: %s", got)
	}
}

func TestValidateSignal(t *testing.T) {
	tests := []struct {
		name string
		req  WebhookRequest
		ok   bool
	}{
		{"valid", WebhookRequest{Pair: "XBT/USD", Type: "buy", Volume: "0.001"}, true},
		{"uppercase side ok", WebhookRequest{Pair: "XBTUSD", Type: "SELL", Volume: "1"}, true},
		{"text only", WebhookRequest{Text: "hello"}, true},
		{"bad side", WebhookRequest{Pair: "XBT/USD", Type: "hold", Volume: "1"}, false},
		{"missing side", WebhookRequest{Pair: "XBT/USD", Volume: "1"}, false},
		{"missing volume", WebhookRequest{Pair: "XBT/USD", Type: "buy"}, false},
		{"bad pair chars", WebhookRequest{Pair: "XBT/USD;DROP", Type: "buy", Volume: "1"}, false},
		{"pair too long", WebhookRequest{Pair: strings.Repeat("A", 40), Type: "buy", Volume: "1"}, false},
		{"zero volume", WebhookRequest{Pair: "XBT/USD", Type: "buy", Volume: "0"}, false},
		{"negative volume", WebhookRequest{Pair: "XBT/USD", Type: "buy", Volume: "-1"}, false},
		{"NaN volume", WebhookRequest{Pair: "XBT/USD", Type: "buy", Volume: "NaN"}, false},
		{"Inf volume", WebhookRequest{Pair: "XBT/USD", Type: "buy", Volume: "Inf"}, false},
		{"+Inf volume", WebhookRequest{Pair: "XBT/USD", Type: "buy", Volume: "+Inf"}, false},
		{"exp volume", WebhookRequest{Pair: "XBT/USD", Type: "buy", Volume: "1e3"}, false},
		{"huge overflow volume", WebhookRequest{Pair: "XBT/USD", Type: "buy", Volume: strings.Repeat("9", 400)}, false},
		{"over cap", WebhookRequest{Pair: "XBT/USD", Type: "buy", Volume: "1000.01"}, false},
		{"at cap", WebhookRequest{Pair: "XBT/USD", Type: "buy", Volume: "1000"}, true},
		{"bad ordertype", WebhookRequest{Pair: "XBT/USD", Type: "buy", Volume: "1", OrderType: "evil"}, false},
		{"limit with price", WebhookRequest{Pair: "XBT/USD", Type: "buy", Volume: "1", OrderType: "limit", Price: "95000.5"}, true},
		{"NaN price", WebhookRequest{Pair: "XBT/USD", Type: "buy", Volume: "1", Price: "NaN"}, false},
		{"relative price", WebhookRequest{Pair: "XBT/USD", Type: "buy", Volume: "1", Price: "+5%"}, true},
		{"bad close price", WebhookRequest{Pair: "XBT/USD", Type: "buy", Volume: "1", ClosePrice: "Inf"}, false},
		{"bad close ordertype", WebhookRequest{Pair: "XBT/USD", Type: "buy", Volume: "1", CloseOrderType: "x"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := tc.req
			err := req.validate(1000)
			if (err == nil) != tc.ok {
				t.Fatalf("ok=%v, err=%v", tc.ok, err)
			}
		})
	}
}

// expectAccepted queues sqlmock expectations for one fully accepted signal.
func expectAccepted(t *testing.T, mock sqlmock.Sqlmock, h *WebhookHandler, id int) {
	mock.ExpectQuery("SELECT (.+) FROM connections WHERE webhook_token").
		WillReturnRows(connectionRow(t, newTestEncryptor(t), 1, "tok", true))
	mock.ExpectQuery("INSERT INTO signals").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(id))
}

func newGuardHandler(t *testing.T, cfg guardConfig) (*WebhookHandler, sqlmock.Sqlmock) {
	db, mock := newMockDB(t)
	h := newWebhookHandlerForTest(t, db, &fakeKraken{}, true)
	h.setGuardConfig(cfg)
	// Text-only/never-trading payloads below skip Kraken; drain on cleanup.
	t.Cleanup(h.Close)
	return h, mock
}

func TestWebhookBodyTooLargeReturns413(t *testing.T) {
	cfg := defaultGuardConfig()
	cfg.MaxBodyBytes = 64
	h, _ := newGuardHandler(t, cfg)
	rec := postWebhook(h, "tok", `{"text":"`+strings.Repeat("a", 200)+`"}`)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d", rec.Code)
	}
}

func TestWebhookInvalidSignalReturns400(t *testing.T) {
	cases := map[string]string{
		"bad side":    `{"pair":"XBT/USD","type":"hold","volume":"1"}`,
		"zero volume": `{"pair":"XBT/USD","type":"buy","volume":"0"}`,
		"NaN volume":  `{"pair":"XBT/USD","type":"buy","volume":"NaN"}`,
		"Inf volume":  `{"pair":"XBT/USD","type":"buy","volume":"Inf"}`,
		"over cap":    `{"pair":"XBT/USD","type":"buy","volume":"5000"}`,
		"bad pair":    `{"pair":"../x","type":"buy","volume":"1"}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := defaultGuardConfig()
			cfg.MaxVolume = 100
			h, _ := newGuardHandler(t, cfg) // no DB expectations: must reject before lookup
			if rec := postWebhook(h, "tok", body); rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestWebhookDuplicateIgnored(t *testing.T) {
	tests := []struct {
		name          string
		first, second string
		wantDup       bool
	}{
		{"identical payload", `{"text":"a"}`, `{"text":"a"}`, true},
		{"different payload", `{"text":"a"}`, `{"text":"b"}`, false},
		{"same idempotency id", `{"text":"a","idempotency_id":"z"}`, `{"text":"b","idempotency_id":"z"}`, true},
		{"different idempotency id", `{"text":"a","idempotency_id":"z"}`, `{"text":"a","idempotency_id":"y"}`, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h, mock := newGuardHandler(t, defaultGuardConfig())
			expectAccepted(t, mock, h, 1)
			if tc.wantDup {
				// second request validates the token but must not save a signal
				mock.ExpectQuery("SELECT (.+) FROM connections WHERE webhook_token").
					WillReturnRows(connectionRow(t, newTestEncryptor(t), 1, "tok", true))
			} else {
				expectAccepted(t, mock, h, 2)
			}
			if rec := postWebhook(h, "tok", tc.first); rec.Code != http.StatusAccepted {
				t.Fatalf("first: %d %s", rec.Code, rec.Body.String())
			}
			rec := postWebhook(h, "tok", tc.second)
			if tc.wantDup {
				if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "duplicate") {
					t.Fatalf("expected 200 duplicate, got %d %s", rec.Code, rec.Body.String())
				}
			} else if rec.Code != http.StatusAccepted {
				t.Fatalf("expected 202, got %d %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestWebhookDuplicateExpiresAfterWindow(t *testing.T) {
	h, mock := newGuardHandler(t, defaultGuardConfig())
	now := time.Unix(1000, 0)
	h.now = func() time.Time { return now }
	expectAccepted(t, mock, h, 1)
	expectAccepted(t, mock, h, 2)
	postWebhook(h, "tok", `{"text":"a"}`)
	now = now.Add(61 * time.Second)
	if rec := postWebhook(h, "tok", `{"text":"a"}`); rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202 after window, got %d", rec.Code)
	}
}

func TestWebhookRateLimit(t *testing.T) {
	tests := []struct {
		name  string
		tweak func(*guardConfig)
	}{
		{"per token", func(c *guardConfig) { c.TokenBurst = 2; c.TokenRatePerMin = 1 }},
		{"per ip", func(c *guardConfig) { c.IPBurst = 2; c.IPRatePerMin = 1 }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := defaultGuardConfig()
			tc.tweak(&cfg)
			h, mock := newGuardHandler(t, cfg)
			h.now = func() time.Time { return time.Unix(1000, 0) }
			expectAccepted(t, mock, h, 1)
			expectAccepted(t, mock, h, 2)
			for i, body := range []string{`{"text":"1"}`, `{"text":"2"}`} {
				if rec := postWebhook(h, "tok", body); rec.Code != http.StatusAccepted {
					t.Fatalf("req %d: expected 202, got %d", i, rec.Code)
				}
			}
			if tc.name == "per token" {
				mock.ExpectQuery("SELECT (.+) FROM connections WHERE webhook_token").
					WillReturnRows(connectionRow(t, newTestEncryptor(t), 1, "tok", true))
			}
			rec := postWebhook(h, "tok", `{"text":"3"}`)
			if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
				t.Fatalf("expected 429 with Retry-After, got %d", rec.Code)
			}
		})
	}
}

func TestClientIPInvalidXFFAndIPv6(t *testing.T) {
	r := httptest.NewRequest("POST", "/", nil)
	r.RemoteAddr = "1.2.3.4:5555"
	r.Header.Set("X-Forwarded-For", "9.9.9.9, garbage")
	if got := clientIP(r, true); got != "1.2.3.4" {
		t.Errorf("invalid XFF should fall back to RemoteAddr, got %s", got)
	}
	r.RemoteAddr = "[2001:db8:1:2:aaaa:bbbb:cccc:dddd]:80"
	a := clientIP(r, false)
	r.RemoteAddr = "[2001:db8:1:2:1111:2222:3333:4444]:80"
	if b := clientIP(r, false); a != b {
		t.Errorf("same /64 should share key: %s vs %s", a, b)
	}
}

func TestValidateNormalizesCloseOrderType(t *testing.T) {
	req := WebhookRequest{CloseOrderType: " Limit ", ClosePrice: " 5 "}
	if err := req.validate(0); err != nil {
		t.Fatal(err)
	}
	if req.CloseOrderType != "limit" || req.ClosePrice != "5" {
		t.Errorf("not normalized: %q %q", req.CloseOrderType, req.ClosePrice)
	}
}
