package kraken

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) (*Kraken, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	k := &Kraken{
		apiKey:     "testkey",
		apiSecret:  "dGVzdHNlY3JldA==", // base64("testsecret")
		baseURL:    srv.URL,
		httpClient: srv.Client(),
	}
	return k, srv
}

func TestGetTicker(t *testing.T) {
	tests := []struct {
		name         string
		pairs        []string
		responseBody string
		wantErr      bool
		wantPair     string
		wantLastPx   string
	}{
		{
			name:         "geldige ticker response",
			pairs:        []string{"XETHZEUR"},
			responseBody: `{"error":[],"result":{"XETHZEUR":{"a":["2217.28","1","1.000"],"b":["2217.27","1","1.000"],"c":["2217.28","0.00270000"],"v":["1234.56","5678.90"],"p":["2210.00","2215.00"],"t":[500,2000],"l":["2100.00","2050.00"],"h":["2300.00","2350.00"],"o":"2150.00"}}}`,
			wantPair:     "XETHZEUR",
			wantLastPx:   "2217.28",
		},
		{
			name:    "lege pairs slice",
			pairs:   []string{},
			wantErr: true,
		},
		{
			name:         "kraken api error",
			pairs:        []string{"INVALIDPAIR"},
			responseBody: `{"error":["EQuery:Unknown asset pair"],"result":null}`,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k, srv := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(tt.responseBody))
			})
			defer srv.Close()

			result, err := k.GetTicker(tt.pairs)
			if tt.wantErr {
				if err == nil {
					t.Error("verwachtte error, kreeg nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("onverwachte error: %v", err)
			}
			info, ok := (*result)[tt.wantPair]
			if !ok {
				t.Fatalf("pair %s niet gevonden in ticker response", tt.wantPair)
			}
			if len(info.LastTrade) == 0 || info.LastTrade[0] != tt.wantLastPx {
				t.Errorf("verwachtte last price %s, kreeg %v", tt.wantLastPx, info.LastTrade)
			}
		})
	}
}
