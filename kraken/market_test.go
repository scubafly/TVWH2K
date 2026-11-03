package kraken

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testClient(t *testing.T, h http.HandlerFunc) *Kraken {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	k, err := NewClient("key", "c2VjcmV0") // base64("secret")
	if err != nil {
		t.Fatal(err)
	}
	k.SetBaseURL(srv.URL)
	return k
}

var testPairs = map[string]AssetPair{
	"XXBTZEUR": {Altname: "XBTEUR", WSName: "XBT/EUR"},
	"XETHZUSD": {Altname: "ETHUSD", WSName: "ETH/USD"},
	"SOLEUR":   {Altname: "SOLEUR", WSName: "SOL/EUR"},
}

func TestNormalizePair(t *testing.T) {
	tests := []struct {
		in, want string
		wantErr  bool
	}{
		{"XXBTZEUR", "XXBTZEUR", false},
		{"XBTEUR", "XXBTZEUR", false},
		{"xbt/eur", "XXBTZEUR", false},
		{"XETHZUSD", "XETHZUSD", false},
		{"ETHUSD", "XETHZUSD", false},
		{"SOLEUR", "SOLEUR", false},
		{"DOGEEUR", "", true},
		{"", "", true},
	}
	for _, tc := range tests {
		got, err := NormalizePair(tc.in, testPairs)
		if (err != nil) != tc.wantErr || got != tc.want {
			t.Errorf("NormalizePair(%q) = %q, %v; want %q (err=%v)", tc.in, got, err, tc.want, tc.wantErr)
		}
		if tc.wantErr && err != nil && tc.in != "" && !strings.Contains(err.Error(), tc.in) {
			t.Errorf("error should name the pair %q: %v", tc.in, err)
		}
	}
}

func TestGetTradesHistoryPaginatesAndSorts(t *testing.T) {
	var offsets []string
	k := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/0/private/TradesHistory" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("API-Sign") == "" {
			t.Error("missing API-Sign")
		}
		r.ParseForm()
		ofs := r.PostForm.Get("ofs")
		offsets = append(offsets, ofs)
		switch ofs {
		case "0":
			w.Write([]byte(`{"error":[],"result":{"count":3,"trades":{"T3":{"pair":"XXBTZEUR","time":300,"type":"sell","price":"3","cost":"3","fee":"0.1","vol":"1"},"T2":{"pair":"XXBTZEUR","time":200,"type":"buy","price":"2","cost":"2","fee":"0.1","vol":"1"}}}}`))
		case "2":
			w.Write([]byte(`{"error":[],"result":{"count":3,"trades":{"T1":{"pair":"XXBTZEUR","time":100,"type":"buy","price":"1","cost":"1","fee":"0","vol":"1"}}}}`))
		default:
			t.Errorf("unexpected ofs %q", ofs)
			w.Write([]byte(`{"error":[],"result":{"count":3,"trades":{}}}`))
		}
	})
	got, err := k.GetTradesHistory()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].ID != "T1" || got[1].ID != "T2" || got[2].ID != "T3" {
		t.Fatalf("unexpected trades %+v", got)
	}
	if len(offsets) != 2 {
		t.Errorf("offsets = %v", offsets)
	}
}

func TestGetTradesHistoryErrors(t *testing.T) {
	for name, tc := range map[string]struct {
		body   string
		status int
	}{
		"api error": {`{"error":["EAPI:Invalid key"]}`, 200},
		"http 500":  {`oops`, 500},
		"bad json":  {`{`, 200},
	} {
		t.Run(name, func(t *testing.T) {
			k := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.body))
			})
			if _, err := k.GetTradesHistory(); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestGetTickerAndAssetPairs(t *testing.T) {
	k := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.Header.Get("API-Key") != "" {
			t.Errorf("public call must be unsigned GET, got %s key=%q", r.Method, r.Header.Get("API-Key"))
		}
		switch r.URL.Path {
		case "/0/public/AssetPairs":
			w.Write([]byte(`{"error":[],"result":{"XXBTZEUR":{"altname":"XBTEUR","wsname":"XBT/EUR"}}}`))
		case "/0/public/Ticker":
			if r.URL.Query().Get("pair") != "XXBTZEUR,SOLEUR" {
				t.Errorf("pair query = %q", r.URL.Query().Get("pair"))
			}
			w.Write([]byte(`{"error":[],"result":{"XXBTZEUR":{"c":["50000.5","0.1"]}}}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	ap, err := k.GetAssetPairs()
	if err != nil || ap["XXBTZEUR"].Altname != "XBTEUR" {
		t.Fatalf("asset pairs: %v %v", ap, err)
	}
	tk, err := k.GetTicker("XXBTZEUR", "SOLEUR")
	if err != nil {
		t.Fatal(err)
	}
	price, err := tk["XXBTZEUR"].LastPrice()
	if err != nil || price != 50000.5 {
		t.Fatalf("price = %v, %v", price, err)
	}
	if _, ok := tk["SOLEUR"]; ok {
		t.Error("SOLEUR should be absent (caller must handle missing pair)")
	}
	if _, err := k.GetTicker(); err == nil {
		t.Error("expected error for no pairs")
	}
}
