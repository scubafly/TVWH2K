// Package kraken provides a client for interacting with the Kraken REST API.
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
	// Je hebt hier mogelijk "encoding/json" nodig als je besluit
	// de basis Kraken error check ({ "error": [...] }) al in doRequest te doen.
	// Voor nu laten we dat over aan de aanroepende functies (AddOrder, GetBalance etc).
)

const (
	// krakenAPIBaseURL is the base URL for all Kraken API V0 calls.
	krakenAPIBaseURL = "https://api.kraken.com"
	// krakenAPIVersionPath is the path prefix for API version 0.
	krakenAPIVersionPath = "/0"
	// krakenPrivatePathPrefix is the complete path prefix for private V0 endpoints.
	krakenPrivatePathPrefix = krakenAPIVersionPath + "/private/"
	// defaultTimeout specifies the default timeout for HTTP requests.
	defaultTimeout = 20 * time.Second
)

// Kraken struct holds the necessary configuration and the HTTP client
// for making authenticated requests to the Kraken API.
type Kraken struct {
	apiKey     string       // Kraken API Key.
	apiSecret  string       // Kraken API Secret (Base64 encoded version).
	httpClient *http.Client // The HTTP client used to make requests.
}

// NewClient initializes and returns a new Kraken API client.
// It requires the API key and the Base64 encoded API secret.
// Returns an error if keys are missing or the secret cannot be decoded.
func NewClient(apiKey, apiSecret string) (*Kraken, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("API key cannot be empty")
	}
	if apiSecret == "" {
		return nil, fmt.Errorf("API secret cannot be empty")
	}

	// Probeer de secret te decoderen om de validiteit te controleren bij initialisatie.
	_, err := base64.StdEncoding.DecodeString(apiSecret)
	if err != nil {
		return nil, fmt.Errorf("invalid base64 API secret: %w. Ensure you use the base64 encoded secret provided by Kraken", err)
	}

	return &Kraken{
		apiKey:    apiKey,
		apiSecret: apiSecret,
		httpClient: &http.Client{
			Timeout: defaultTimeout, // Gebruik een redelijke timeout.
		},
	}, nil
}

// generateSignature creates the API-Sign header value according to Kraken's specifications.
// This function is intended for internal use within the package.
// path: The full API endpoint path (e.g., "/0/private/AddOrder").
// nonce: The unique nonce string used for this request.
// urlEncodedData: The URL-encoded string of POST parameters (e.g., "nonce=123&pair=XBTUSD...").
func (k *Kraken) generateSignature(path string, nonce string, urlEncodedData string) (string, error) {
	// 1. Decode Base64 secret. Check is al in NewClient gedaan, maar double check kan geen kwaad.
	secretBytes, err := base64.StdEncoding.DecodeString(k.apiSecret)
	if err != nil {
		// Should not happen if NewClient succeeded, indicates internal state issue.
		return "", fmt.Errorf("internal error: could not decode API secret: %w", err)
	}

	// 2. Calculate SHA256 hash of (nonce + POST data string).
	sha := sha256.New()
	// Belangrijk: Eerst nonce, dan de encoded data string.
	if _, err := sha.Write([]byte(nonce + urlEncodedData)); err != nil {
		return "", fmt.Errorf("internal error: failed writing to sha256: %w", err)
	}
	hashSum := sha.Sum(nil)

	// 3. Calculate HMAC-SHA512 of (API path + SHA256(nonce + POST data)) using the decoded secret.
	mac := hmac.New(sha512.New, secretBytes)
	// Belangrijk: Eerst het pad, dan de hash van stap 2.
	if _, err := mac.Write([]byte(path)); err != nil {
		return "", fmt.Errorf("internal error: failed writing path to hmac: %w", err)
	}
	if _, err := mac.Write(hashSum); err != nil {
		return "", fmt.Errorf("internal error: failed writing hashSum to hmac: %w", err)
	}
	macSum := mac.Sum(nil)

	// 4. Base64 encode the HMAC result.
	signature := base64.StdEncoding.EncodeToString(macSum)
	return signature, nil
}

// doRequest performs the actual HTTP request to a private Kraken API endpoint.
// It handles nonce generation, signature creation, header setting, and request execution.
// This function is intended for internal use within the package.
// method: The HTTP method (usually "POST" for Kraken private endpoints).
// path: The specific API endpoint path (e.g., "/0/private/Balance").
// params: The request parameters as url.Values.
// Returns the raw response body bytes and an error if any step fails.
func (k *Kraken) doRequest(method, path string, params url.Values) ([]byte, error) {
	// Controleer of het pad correct begint.
	if !strings.HasPrefix(path, krakenPrivatePathPrefix) {
		return nil, fmt.Errorf("internal logic error: path '%s' does not match private endpoint prefix '%s'", path, krakenPrivatePathPrefix)
	}
	fullURL := krakenAPIBaseURL + path

	// Zorg dat params nooit nil is, voorkomt nil pointer dereference.
	if params == nil {
		params = url.Values{}
	}

	// Genereer een unieke nonce (altijd nodig voor private calls).
	nonce := fmt.Sprintf("%d", time.Now().UnixNano())
	params.Set("nonce", nonce)

	// Encodeer de parameters naar application/x-www-form-urlencoded formaat.
	encodedParams := params.Encode()
	requestBody := strings.NewReader(encodedParams)

	// Maak het http.Request object.
	req, err := http.NewRequest(method, fullURL, requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create new HTTP request for %s: %w", path, err)
	}

	// Genereer de API signature.
	apiSign, err := k.generateSignature(path, nonce, encodedParams)
	if err != nil {
		// Fout bij signature generatie duidt meestal op een programmeerfout of config issue.
		return nil, fmt.Errorf("failed to generate API signature for %s: %w", path, err)
	}

	// Stel de vereiste HTTP headers in.
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json") // We verwachten JSON terug.
	req.Header.Set("API-Key", k.apiKey)
	req.Header.Set("API-Sign", apiSign)
	// Een User-Agent is goede praktijk.
	req.Header.Set("User-Agent", "YourApp/1.0 (Go Kraken Client)")

	// Voer de request uit via de http client.
	// fmt.Printf("DEBUG :: Requesting %s %s with Nonce %s\n", method, fullURL, nonce) // Uncomment voor debuggen
	res, err := k.httpClient.Do(req)
	if err != nil {
		// Dit zijn meestal netwerkfouten, timeouts, DNS problemen etc.
		return nil, fmt.Errorf("HTTP request execution failed for %s: %w", path, err)
	}
	defer res.Body.Close() // Altijd de body sluiten!

	// Lees de response body.
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body from %s: %w", path, err)
	}

	// Controleer op non-success HTTP status codes. Kraken gebruikt vaak 200 OK,
	// maar een 5xx of 4xx kan wijzen op problemen buiten de API zelf (Cloudflare, rate limits, etc.).
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return body, fmt.Errorf("received non-2xx HTTP status %d from %s: %s", res.StatusCode, path, string(body))
	}

	// BELANGRIJK: Deze functie controleert *niet* op Kraken-specifieke errors in de JSON body
	// zoals {"error": ["EOrder:Invalid price"]}. Dit moet gebeuren in de aanroepende
	// functies (AddOrder, GetBalance, etc.) omdat die ook de specifieke "result" structuur
	// moeten parsen. Zij zijn verantwoordelijk voor het volledig interpreteren van de JSON response.

	// Retourneer de ruwe body bytes bij een succesvolle HTTP transactie.
	return body, nil
}

// SetHttpClient allows replacing the default HTTP client.
// This is useful for testing or advanced configurations like setting proxies
// or custom transport layers.
func (k *Kraken) SetHttpClient(client *http.Client) {
	if client != nil {
		k.httpClient = client
	}
}
