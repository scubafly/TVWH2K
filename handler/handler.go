package handler

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/joho/godotenv"
)

type WebhookHandler struct {
	// Initialize your storage and other necessary fields here.
}

func NewWebhookHandler() *WebhookHandler {
	return &WebhookHandler{}
}

func (h *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	err := godotenv.Load(".env")
	if err != nil {
		log.Fatalf("Error loading .env file")
	}
	var requestBody map[string]string

	err = json.NewDecoder(r.Body).Decode(&requestBody)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	token, ok := requestBody["token"]
	if !ok || token != os.Getenv("TOKEN") {
		fmt.Println("Invalid token", string(token))
		return
	}

	fmt.Println("ok: ", ok)
	fmt.Println("Token is: ", string(requestBody["token"]))
	fmt.Println("Data is: ", string(requestBody["text"]))
}
