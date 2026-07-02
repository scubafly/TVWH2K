package telegram

import (
	"encoding/json"
	"os"
	"strconv"
	"testing"
)

type Response struct {
	OK     bool `json:"ok"`
	Result struct {
		MessageID int `json:"message_id"`
		From      struct {
			ID        int    `json:"id"`
			IsBot     bool   `json:"is_bot"`
			FirstName string `json:"first_name"`
			Username  string `json:"username"`
		} `json:"from"`
		Chat struct {
			ID        int    `json:"id"`
			FirstName string `json:"first_name"`
			LastName  string `json:"last_name"`
			Username  string `json:"username"`
			Type      string `json:"type"`
		} `json:"chat"`
		Date int64  `json:"date"`
		Text string `json:"text"`
	} `json:"result"`
}

// TestSendMessage hits the real Telegram API, so it only runs when
// TELEGRAM_BOT_TOKEN/TELEGRAM_CHAT_ID are set in the environment.
func TestSendMessage(t *testing.T) {
	botToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	chatIDStr := os.Getenv("TELEGRAM_CHAT_ID")
	if botToken == "" || chatIDStr == "" {
		t.Skip("TELEGRAM_BOT_TOKEN/TELEGRAM_CHAT_ID not set, skipping live Telegram test")
	}

	chatID, err := strconv.ParseInt(chatIDStr, 10, 64)
	if err != nil {
		t.Fatalf("error converting TELEGRAM_CHAT_ID to int: %s", err.Error())
	}

	text := "Running unit test"
	resp, err := SendMessage(botToken, text, chatID)
	if err != nil {
		t.Fatalf("Error sending message to Telegram, got %s", err.Error())
	}

	var respData Response
	if err := json.Unmarshal([]byte(resp), &respData); err != nil {
		t.Fatalf("Error unmarshalling response from Telegram, got %s", err.Error())
	}
	if text != respData.Result.Text {
		t.Errorf("Expected response to be %s, got %s", text, respData.Result.Text)
	}
}
