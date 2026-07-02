package telegram

import (
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var httpClient = &http.Client{Timeout: 10 * time.Second}

// SendMessage posts text to the given Telegram chat using botToken.
// botToken is per-connection (each user can wire their own bot), so it's
// passed in rather than read from a global env var.
func SendMessage(botToken, text string, chatId int64) (string, error) {
	apiUrl := "https://api.telegram.org/bot" + botToken + "/sendMessage"

	message := url.Values{
		"text":    {text},
		"chat_id": {strconv.FormatInt(chatId, 10)},
	}

	req, err := http.NewRequest(
		http.MethodPost,
		apiUrl,
		strings.NewReader(message.Encode()),
	)
	if err != nil {
		log.Printf("Error building Telegram request: %s", err)
		return "", err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := httpClient.Do(req)
	if err != nil {
		log.Printf("Error sending Telegram message: %s", err)
		return "", err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Error reading Telegram response: %s", err)
		return "", err
	}

	return string(bodyBytes), nil
}
