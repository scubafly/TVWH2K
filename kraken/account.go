package kraken

import (
	"encoding/json"
	"fmt"
	"net/url"
	// We gaan ervan uit dat de benodigde response types zoals
	// BalanceResponse, TradeBalanceResponse, en de helper GenericResponse
	// gedefinieerd zijn in het bestand types.go binnen dezelfde package.
)

// GetBalance haalt de actuele account balans op voor alle valuta.
// Deze methode correspondeert met het Kraken API endpoint: /0/private/Balance
// Het retourneert een pointer naar een BalanceResponse (gedefinieerd in types.go,
// typisch een map[string]string van asset naam naar balans) en een error.
func (k *Kraken) GetBalance() (*BalanceResponse, error) {
	// Specifiek API pad voor deze functie.
	path := krakenPrivatePathPrefix + "Balance"

	// Voor de Balance endpoint zijn geen extra parameters nodig naast de 'nonce'
	// die automatisch door doRequest wordt toegevoegd.
	params := url.Values{}

	// Roep de interne doRequest methode aan om de API call uit te voeren.
	respBytes, err := k.doRequest("POST", path, params)
	if err != nil {
		// Er is iets misgegaan tijdens de HTTP request zelf (netwerk, signature, etc.).
		return nil, fmt.Errorf("kraken request voor GetBalance mislukt: %w", err)
	}

	// --- Verwerk de ontvangen response bytes ---

	// 1. Probeer de response te parsen in de generieke structuur om errors te detecteren.
	var genericResp GenericResponse // GenericResponse moet gedefinieerd zijn in types.go
	if err := json.Unmarshal(respBytes, &genericResp); err != nil {
		// De response was geen valide JSON of had niet de verwachte structuur.
		return nil, fmt.Errorf("parsen van generieke response voor GetBalance mislukt: %w. Body: %s", err, string(respBytes))
	}

	// 2. Controleer of Kraken specifieke errors heeft geretourneerd in het 'error' veld.
	if len(genericResp.Error) > 0 {
		return nil, fmt.Errorf("kraken API retourneerde fouten voor GetBalance: %v", genericResp.Error)
	}

	// 3. Controleer of het 'result' veld aanwezig is.
	if genericResp.Result == nil {
		// Soms retourneert Kraken een succesvolle call zonder resultaat (onverwacht hier).
		return nil, fmt.Errorf("kraken API response voor GetBalance bevat geen 'result' veld. Body: %s", string(respBytes))
	}

	// 4. Parse het 'result' veld (json.RawMessage) naar de specifieke BalanceResponse struct.
	var balanceResult BalanceResponse // BalanceResponse is gedefinieerd in types.go
	if err := json.Unmarshal(genericResp.Result, &balanceResult); err != nil {
		// Het 'result' veld had niet de verwachte structuur voor balansen.
		return nil, fmt.Errorf("parsen van balans resultaat voor GetBalance mislukt: %w. Result Body: %s", err, string(genericResp.Result))
	}

	// Alles ging goed, retourneer de geparste balans.
	return &balanceResult, nil
}

// GetTradeBalance haalt de trade balans informatie op ( equity, free margin etc. ).
// Deze methode correspondeert met het Kraken API endpoint: /0/private/TradeBalance
// optionalAsset: Optionele parameter om de balans in een specifieke valuta te berekenen (default: ZUSD).
// Retourneert een pointer naar TradeBalanceResponse (gedefinieerd in types.go) en een error.
func (k *Kraken) GetTradeBalance(optionalAsset string) (*TradeBalanceResponse, error) {
	path := krakenPrivatePathPrefix + "TradeBalance"
	params := url.Values{}

	// Voeg de optionele 'asset' parameter toe indien meegegeven.
	if optionalAsset != "" {
		params.Set("asset", optionalAsset)
	}

	// Roep de interne doRequest methode aan.
	respBytes, err := k.doRequest("POST", path, params)
	if err != nil {
		return nil, fmt.Errorf("kraken request voor GetTradeBalance mislukt: %w", err)
	}

	// Verwerk de response (zelfde stappen als bij GetBalance).
	var genericResp GenericResponse
	if err := json.Unmarshal(respBytes, &genericResp); err != nil {
		return nil, fmt.Errorf("parsen van generieke response voor GetTradeBalance mislukt: %w. Body: %s", err, string(respBytes))
	}
	if len(genericResp.Error) > 0 {
		return nil, fmt.Errorf("kraken API retourneerde fouten voor GetTradeBalance: %v", genericResp.Error)
	}
	if genericResp.Result == nil {
		return nil, fmt.Errorf("kraken API response voor GetTradeBalance bevat geen 'result' veld. Body: %s", string(respBytes))
	}

	// Parse het resultaat naar de TradeBalanceResponse struct.
	var tradeBalanceResult TradeBalanceResponse // TradeBalanceResponse is gedefinieerd in types.go
	if err := json.Unmarshal(genericResp.Result, &tradeBalanceResult); err != nil {
		return nil, fmt.Errorf("parsen van trade balans resultaat voor GetTradeBalance mislukt: %w. Result Body: %s", err, string(genericResp.Result))
	}

	return &tradeBalanceResult, nil
}

/*
// Je kunt hier meer functies toevoegen voor andere account-gerelateerde endpoints:
// Bijvoorbeeld:
func (k *Kraken) GetLedgers(params LedgersInput) (*LedgersResponse, error) {
    path := krakenPrivatePathPrefix + "Ledgers"
    // Converteer LedgersInput naar url.Values
    urlParams := // ... conversie logica ...
    respBytes, err := k.doRequest("POST", path, urlParams)
    // ... response parsing ...
}

func (k *Kraken) QueryLedgers(ledgerIDs []string) (*LedgersResponse, error) {
    path := krakenPrivatePathPrefix + "QueryLedgers"
    params := url.Values{}
    params.Set("id", strings.Join(ledgerIDs, ",")) // IDs gescheiden door komma's
    respBytes, err := k.doRequest("POST", path, params)
    // ... response parsing ...
}
*/
