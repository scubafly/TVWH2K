// Package kraken provides a client for interacting with the Kraken REST API.
package kraken

import (
	"encoding/json"
	"fmt"
	"strings"
)

// APIError represents one or more errors returned directly by the Kraken API
// within the "error" field of the standard JSON response structure.
type APIError struct {
	// Messages contains the list of error strings provided by the Kraken API.
	// Examples: ["EGeneral:Invalid arguments", "EService:Unavailable"]
	Messages []string
}

// Error implements the standard Go error interface for APIError.
// It returns a string representation of the Kraken API error(s).
func (e *APIError) Error() string {
	// If somehow created without messages, return a generic message.
	if len(e.Messages) == 0 {
		return "unknown Kraken API error occurred"
	}

	// Join multiple error messages returned by Kraken for a comprehensive view.
	// Use a semicolon and space as a separator.
	return fmt.Sprintf("kraken API error(s): %s", strings.Join(e.Messages, "; "))
}

// IsKrakenError checks if a given error is specifically a Kraken APIError.
// This helps distinguish Kraken API errors from network errors, timeouts, etc.
func IsKrakenError(err error) bool {
	if err == nil {
		return false
	}
	_, ok := err.(*APIError)
	return ok
}

// parseKrakenError is an internal helper function (not exported) designed to
// check the raw response body from Kraken for API-level errors defined in the
// standard `{"error": [...]}` format.
//
// If the body contains Kraken errors, it returns an *APIError populated with those messages.
// If the body cannot be parsed as JSON containing an "error" field, or if the "error" field
// is present but empty, it returns nil, indicating no *Kraken API specific* errors were found.
//
// This function should typically be called within the public API methods (like AddOrder, GetBalance)
// after receiving the response bytes from doRequest, but before attempting to parse the "result" field.
func parseKrakenError(body []byte) error {
	// We only care about the "error" field for this function.
	// Using an anonymous struct avoids needing the full GenericResponse type here.
	var krakenErrorResponse struct {
		// Use `json:"error"` tag to match the Kraken API response field name.
		ErrorMessages []string `json:"error"`
	}

	// Attempt to unmarshal the response body into our minimal structure.
	// If this fails, it's likely not a standard Kraken JSON error response
	// (e.g., HTML from Cloudflare, malformed JSON). In this case, we assume
	// it's not a reportable Kraken API error according to their standard format.
	// The calling function might have already caught HTTP status errors from doRequest.
	if err := json.Unmarshal(body, &krakenErrorResponse); err != nil {
		// Cannot parse as expected JSON, so no Kraken API errors detected by this function.
		return nil
	}

	// Check if the "error" array actually contains any error strings.
	if len(krakenErrorResponse.ErrorMessages) > 0 {
		// Yes, Kraken reported errors. Return them wrapped in our custom APIError type.
		return &APIError{
			Messages: krakenErrorResponse.ErrorMessages,
		}
	}

	// No errors found in the "error" field.
	return nil
}

/*
// --- Optional: Define specific error variables ---
// For very common errors, you could define specific variables.
// This allows checks like `if errors.Is(err, kraken.ErrRateLimitExceeded)`.
// Note: This requires implementing an `Is(target error) bool` method on APIError
// or careful error wrapping/checking.

var (
	// ErrPermissionDeniedExample represents a possible structure for a predefined error.
	// The exact message string needs to match Kraken's output.
	ErrPermissionDeniedExample = &APIError{Messages: []string{"EGeneral:Permission denied"}}

	// ErrRateLimitExceededExample represents another potential predefined error.
	ErrRateLimitExceededExample = &APIError{Messages: []string{"EAPI:Rate limit exceeded"}}
)

// Example implementation of Is for APIError (requires Go 1.13+)
func (e *APIError) Is(target error) bool {
	// Check if the target is also an *APIError
	targetAPIError, ok := target.(*APIError)
	if !ok {
		return false
	}
	// If the target has no specific messages, any APIError matches
	if len(targetAPIError.Messages) == 0 {
		return true
	}
	// Check if *any* of our messages match the *first* message in the target.
	// (Kraken error codes are usually the first part of the first message).
	// A more robust check might compare specific error codes if they were parsed.
	if len(e.Messages) > 0 && len(targetAPIError.Messages) > 0 {
		// Simple prefix check might be useful, e.g., checking for "EAPI:", "EGeneral:"
		return strings.HasPrefix(e.Messages[0], targetAPIError.Messages[0]) || e.Messages[0] == targetAPIError.Messages[0]

	}
	return false
}
*/
