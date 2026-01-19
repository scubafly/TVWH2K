package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"tvwh2k/database"
	"tvwh2k/handler"
	"tvwh2k/kraken"
)

func main() {
	apiKey := os.Getenv("KRAKEN_API_KEY")
	apiSecret := os.Getenv("KRAKEN_API_SECRET")

	var k *kraken.Kraken
	var err error

	if apiKey == "" || apiSecret == "" {
		fmt.Println("Warning: KRAKEN_API_KEY or KRAKEN_API_SECRET not set. Kraken integration disabled.")
	} else {
		k, err = kraken.NewClient(apiKey, apiSecret)
		if err != nil {
			log.Fatalf("Failed to create Kraken client: %v", err)
		}
		fmt.Println("Kraken client initialized.")
	}

	// Initialize Database
	db, err := database.InitDB("./tvwh2k.db")
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}

	h := handler.NewWebhookHandler(k, db)
	http.HandleFunc("/webhooks", h.ServeHTTP)
	http.HandleFunc("/api/signals", h.HandleGetSignals)
	http.HandleFunc("/api/trades", h.HandleGetTrades)

	fmt.Println("Starting server on :8081...")
	if err := http.ListenAndServe(":8081", nil); err != nil {
		log.Fatal(err)
	}
}
