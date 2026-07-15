package kraken

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	krakenAPIBaseURL        = "https://api.kraken.com"
	krakenAPIVersionPath    = "/0"
	krakenPrivatePathPrefix = krakenAPIVersionPath + "/private/"
	defaultTimeout          = 20 * time.Second
)

// Kraken holds the API credentials and HTTP client for authenticated requests.
type Kraken struct {
	apiKey     string
	apiSecret  string // Base64-encoded secret as provided by Kraken.
	httpClient *http.Client
	baseURL    string // Override in tests via SetBaseURL.
}

// NewClient initializes a Kraken API client. The apiSecret must be the
// Base64-encoded string from the Kraken API settings page.
func NewClient(apiKey, apiSecret string) (*Kraken, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("API key cannot be empty")
	}
	if apiSecret == "" {
		return nil, fmt.Errorf("API secret cannot be empty")
	}
	if _, err := base64.StdEncoding.DecodeString(apiSecret); err != nil {
		return nil, fmt.Errorf("invalid base64 API secret: %w", err)
	}
	return &Kraken{
		apiKey:    apiKey,
		apiSecret: apiSecret,
		baseURL:   krakenAPIBaseURL,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
	}, nil
}

// generateSignature creates the API-Sign header value per Kraken's spec:
// Base64(HMAC-SHA512(path + SHA256(nonce + postData), decodedSecret))
func (k *Kraken) generateSignature(path, nonce, urlEncodedData string) (string, error) {
	secretBytes, err := base64.StdEncoding.DecodeString(k.apiSecret)
	if err != nil {
		return "", fmt.Errorf("could not decode API secret: %w", err)
	}

	sha := sha256.New()
	if _, err := sha.Write([]byte(nonce + urlEncodedData)); err != nil {
		return "", fmt.Errorf("sha256 write failed: %w", err)
	}

	mac := hmac.New(sha512.New, secretBytes)
	if _, err := mac.Write([]byte(path)); err != nil {
		return "", fmt.Errorf("hmac write path failed: %w", err)
	}
	if _, err := mac.Write(sha.Sum(nil)); err != nil {
		return "", fmt.Errorf("hmac write hash failed: %w", err)
	}

	return base64.StdEncoding.EncodeToString(mac.Sum(nil)), nil
}

// doRequest executes an authenticated POST to a private Kraken endpoint.
func (k *Kraken) doRequest(method, path string, params url.Values) ([]byte, error) {
	if !strings.HasPrefix(path, krakenPrivatePathPrefix) {
		return nil, fmt.Errorf("path %q does not start with private prefix %q", path, krakenPrivatePathPrefix)
	}

	if params == nil {
		params = url.Values{}
	}

	nonce := fmt.Sprintf("%d", time.Now().UnixNano())
	params.Set("nonce", nonce)
	encodedParams := params.Encode()

	req, err := http.NewRequest(method, k.baseURL+path, strings.NewReader(encodedParams))
	if err != nil {
		return nil, fmt.Errorf("failed to create request for %s: %w", path, err)
	}

	apiSign, err := k.generateSignature(path, nonce, encodedParams)
	if err != nil {
		return nil, fmt.Errorf("failed to generate signature for %s: %w", path, err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("API-Key", k.apiKey)
	req.Header.Set("API-Sign", apiSign)
	req.Header.Set("User-Agent", "TradeBridge/1.0")

	res, err := k.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed for %s: %w", path, err)
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body from %s: %w", path, err)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return body, fmt.Errorf("non-2xx status %d from %s: %s", res.StatusCode, path, string(body))
	}

	return body, nil
}

// doPublicGet executes an unauthenticated GET to a public Kraken endpoint.
func (k *Kraken) doPublicGet(path string, params url.Values) ([]byte, error) {
	fullURL := k.baseURL + path
	if len(params) > 0 {
		fullURL += "?" + params.Encode()
	}

	req, err := http.NewRequest("GET", fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request for %s: %w", path, err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "TradeBridge/1.0")

	res, err := k.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed for %s: %w", path, err)
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body from %s: %w", path, err)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return body, fmt.Errorf("non-2xx status %d from %s: %s", res.StatusCode, path, string(body))
	}

	return body, nil
}

// SetHttpClient replaces the default HTTP client. Useful for testing or custom transports.
func (k *Kraken) SetHttpClient(client *http.Client) {
	if client != nil {
		k.httpClient = client
	}
}

// SetBaseURL overrides the Kraken API base URL. Intended for testing only.
func (k *Kraken) SetBaseURL(url string) {
	k.baseURL = url
}
