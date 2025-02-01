package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"tvwh2k/telegram"
)

type WebhookHandler struct {
	// Initialize your storage and other necessary fields here.
}

func NewWebhookHandler() *WebhookHandler {
	return &WebhookHandler{}
}

func (h *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var requestBody map[string]string

	err := json.NewDecoder(r.Body).Decode(&requestBody)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	token, ok := requestBody["token"]
	if !ok || token != os.Getenv("TOKEN") {
		fmt.Println("Invalid token", string(token))
		fmt.Println("Debug", os.Getenv("TOKEN"))
		if os.Getenv("DEBUG_MODE") == "true" {
			fmt.Println("Expected", os.Getenv("TOKEN"))
		}
		return
	}

	fmt.Println("Token is: ", string(requestBody["token"]))
	fmt.Println("Data is: ", string(requestBody["text"]))

	chat_id, err := strconv.Atoi(os.Getenv("TELEGRAM_CHAT_ID"))
	if err != nil {
		fmt.Println("Error not chat id.");
		return;
	}

	message := requestBody["text"]
	telegram.SendMessage(message, int64(chat_id))
}
