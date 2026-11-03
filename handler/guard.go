package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// guardConfig holds the webhook hardening knobs (see README "Webhook
// hardening"). All values come from env vars with safe defaults.
type guardConfig struct {
	TokenRatePerMin float64       // WEBHOOK_TOKEN_RATE_PER_MIN
	TokenBurst      int           // WEBHOOK_TOKEN_BURST
	IPRatePerMin    float64       // WEBHOOK_IP_RATE_PER_MIN
	IPBurst         int           // WEBHOOK_IP_BURST
	DedupWindow     time.Duration // WEBHOOK_DEDUP_WINDOW (0 disables)
	MaxBodyBytes    int64         // WEBHOOK_MAX_BODY_BYTES
	MaxVolume       float64       // WEBHOOK_MAX_VOLUME
	TrustProxy      bool          // WEBHOOK_TRUST_PROXY: use last X-Forwarded-For hop as client IP
}

func defaultGuardConfig() guardConfig {
	return guardConfig{
		TokenRatePerMin: 30,
		TokenBurst:      10,
		IPRatePerMin:    120,
		IPBurst:         30,
		DedupWindow:     60 * time.Second,
		MaxBodyBytes:    16 * 1024,
		MaxVolume:       1000000,
	}
}

func guardConfigFromEnv() guardConfig {
	c := defaultGuardConfig()
	envFloat := func(name string, dst *float64) {
		if v := os.Getenv(name); v != "" {
			f, err := strconv.ParseFloat(v, 64)
			if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || f <= 0 {
				log.Printf("Ignoring invalid %s=%q", name, v)
				return
			}
			*dst = f
		}
	}
	envInt := func(name string, dst *int) {
		if v := os.Getenv(name); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n <= 0 {
				log.Printf("Ignoring invalid %s=%q", name, v)
				return
			}
			*dst = n
		}
	}
	envFloat("WEBHOOK_TOKEN_RATE_PER_MIN", &c.TokenRatePerMin)
	envInt("WEBHOOK_TOKEN_BURST", &c.TokenBurst)
	envFloat("WEBHOOK_IP_RATE_PER_MIN", &c.IPRatePerMin)
	envInt("WEBHOOK_IP_BURST", &c.IPBurst)
	envFloat("WEBHOOK_MAX_VOLUME", &c.MaxVolume)
	if v := os.Getenv("WEBHOOK_MAX_BODY_BYTES"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			c.MaxBodyBytes = n
		} else {
			log.Printf("Ignoring invalid WEBHOOK_MAX_BODY_BYTES=%q", v)
		}
	}
	if v := os.Getenv("WEBHOOK_DEDUP_WINDOW"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d >= 0 {
			c.DedupWindow = d
		} else {
			log.Printf("Ignoring invalid WEBHOOK_DEDUP_WINDOW=%q", v)
		}
	}
	c.TrustProxy = os.Getenv("WEBHOOK_TRUST_PROXY") == "true"
	return c
}

// --- token-bucket rate limiter ---

type bucket struct {
	tokens float64
	last   time.Time
}

// rateLimiter is an in-memory token bucket per key.
type rateLimiter struct {
	mu        sync.Mutex
	perSec    float64
	burst     float64
	buckets   map[string]*bucket
	lastSweep time.Time
}

func newRateLimiter(perMin float64, burst int) *rateLimiter {
	return &rateLimiter{perSec: perMin / 60, burst: float64(burst), buckets: make(map[string]*bucket)}
}

// allow takes one token for key. When denied it returns how long to wait.
func (l *rateLimiter) allow(key string, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Drop buckets that have fully refilled (idle) so the map can't grow forever.
	if now.Sub(l.lastSweep) > time.Minute {
		for k, b := range l.buckets {
			if b.tokens+now.Sub(b.last).Seconds()*l.perSec >= l.burst {
				delete(l.buckets, k)
			}
		}
		l.lastSweep = now
	}

	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}
	b.tokens = math.Min(l.burst, b.tokens+now.Sub(b.last).Seconds()*l.perSec)
	b.last = now
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	return false, time.Duration((1 - b.tokens) / l.perSec * float64(time.Second))
}

// --- dedup ---

type deduper struct {
	mu        sync.Mutex
	window    time.Duration
	seen      map[string]time.Time
	lastSweep time.Time
}

func newDeduper(window time.Duration) *deduper {
	return &deduper{window: window, seen: make(map[string]time.Time)}
}

// firstSeen records key and reports true, or false if key was already seen
// within the window.
func (d *deduper) firstSeen(key string, now time.Time) bool {
	if d.window <= 0 {
		return true
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if now.Sub(d.lastSweep) > d.window {
		for k, t := range d.seen {
			if now.Sub(t) >= d.window {
				delete(d.seen, k)
			}
		}
		d.lastSweep = now
	}
	if t, ok := d.seen[key]; ok && now.Sub(t) < d.window {
		return false
	}
	d.seen[key] = now
	return true
}

// forget releases key (used when the signal could not be queued, so a retry
// from the sender isn't mistaken for a duplicate).
func (d *deduper) forget(key string) {
	d.mu.Lock()
	delete(d.seen, key)
	d.mu.Unlock()
}

// dedupKey scopes the key to the token. An explicit idempotency_id wins;
// otherwise the key is a hash of the normalized payload.
func dedupKey(token string, req WebhookRequest) string {
	if req.IdempotencyID != "" {
		return token + "|id|" + req.IdempotencyID
	}
	b, _ := json.Marshal(req)
	sum := sha256.Sum256(b)
	return token + "|body|" + hex.EncodeToString(sum[:])
}

// --- client IP ---

func clientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if ip := net.ParseIP(strings.TrimSpace(parts[len(parts)-1])); ip != nil {
				return ipKey(ip)
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip != nil {
		return ipKey(ip)
	}
	return host
}

// ipKey normalizes an IP for rate limiting: IPv4 as-is, IPv6 masked to /64
// so one host can't dodge limits by rotating addresses within its prefix.
func ipKey(ip net.IP) string {
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	return ip.Mask(net.CIDRMask(64, 128)).String() + "/64"
}

// --- validation ---

var (
	pairRe    = regexp.MustCompile(`^[A-Za-z0-9]{2,12}(/[A-Za-z0-9]{2,12})?$`)
	decimalRe = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?$`)
	// Kraken price fields may be relative ("+100", "-5%", "#50").
	priceRe = regexp.MustCompile(`^[+\-#]?[0-9]+(\.[0-9]+)?%?$`)

	validOrderTypes = map[string]bool{
		"market": true, "limit": true, "stop-loss": true, "take-profit": true,
		"stop-loss-limit": true, "take-profit-limit": true, "trailing-stop": true,
		"trailing-stop-limit": true, "iceberg": true, "settle-position": true,
	}
)

// validate checks and normalizes the signal in place. A payload with no
// pair/type/volume is a text-only notification and is allowed; otherwise all
// three must be present and sane.
func (req *WebhookRequest) validate(maxVolume float64) error {
	req.Type = strings.ToLower(strings.TrimSpace(req.Type))
	req.Pair = strings.TrimSpace(req.Pair)
	req.Volume = strings.TrimSpace(req.Volume)
	req.OrderType = strings.ToLower(strings.TrimSpace(req.OrderType))
	req.CloseOrderType = strings.ToLower(strings.TrimSpace(req.CloseOrderType))
	req.Price = strings.TrimSpace(req.Price)
	req.Price2 = strings.TrimSpace(req.Price2)
	req.ClosePrice = strings.TrimSpace(req.ClosePrice)
	req.ClosePrice2 = strings.TrimSpace(req.ClosePrice2)

	if len(req.IdempotencyID) > 128 {
		return fmt.Errorf("idempotency_id too long")
	}

	if req.OrderType != "" && !validOrderTypes[req.OrderType] {
		return fmt.Errorf("invalid ordertype %q", req.OrderType)
	}
	for name, v := range map[string]string{
		"price": req.Price, "price2": req.Price2,
		"close_price": req.ClosePrice, "close_price2": req.ClosePrice2,
	} {
		if v != "" && !priceRe.MatchString(v) {
			return fmt.Errorf("invalid %s", name)
		}
	}
	if req.CloseOrderType != "" && !validOrderTypes[req.CloseOrderType] {
		return fmt.Errorf("invalid close_ordertype %q", req.CloseOrderType)
	}

	if req.Pair == "" && req.Type == "" && req.Volume == "" {
		return nil
	}
	if req.Type != "buy" && req.Type != "sell" {
		return fmt.Errorf("type must be buy or sell")
	}
	if !pairRe.MatchString(req.Pair) {
		return fmt.Errorf("invalid pair")
	}
	if !decimalRe.MatchString(req.Volume) {
		return fmt.Errorf("volume must be a plain decimal number")
	}
	vol, err := strconv.ParseFloat(req.Volume, 64)
	if err != nil || math.IsNaN(vol) || math.IsInf(vol, 0) || vol <= 0 {
		return fmt.Errorf("volume must be > 0")
	}
	if maxVolume > 0 && vol > maxVolume {
		return fmt.Errorf("volume exceeds maximum of %v", maxVolume)
	}
	return nil
}
