package handler

import (
	"fmt"
	"io"
	"log"
	"net/http"
)

type WebhookHandler struct {
	// Initialize your storage and other necessary fields here.
}

func NewWebhookHandler() *WebhookHandler {
	// This function should return a new WebhookHandler object
	return &WebhookHandler{}
}

func (h *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, err := io.ReadAll(r.Body)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Data is: ", string(b))
}
