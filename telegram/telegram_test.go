package telegram

import (
	"encoding/json"
	"log"
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

func TestSendMessage(t *testing.T) {

	text := "Runing unit test"
	chat_id, err := strconv.Atoi(os.Getenv("TELEGRAM_CHAT_ID"))
	if err != nil {
		t.Errorf("error converting sting to int", err.Error())
	}

	resp, err := SendMessage(text, chat_id))
	if err != nil {
		t.Errorf("Error sending message to Telegram, got %s", err.Error())
	}

	var respData Response
	err = json.Unmarshal([]byte(resp), &respData)
	if err != nil {
		t.Errorf("Error unmarshalling response from Telegram, got %s", err.Error())
	}
	// check := strings.Split(resp, "")[4]
	if text != respData.Result.Text {
		t.Errorf("Expected response to be %s, got %s", text, respData.Result.Text)
	}

}
