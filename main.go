package main

import (
	"log"
	"net/http"
	"os"
	"tvwh2k/database"
	"tvwh2k/handler"
	"tvwh2k/kraken"

	"github.com/joho/godotenv"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, falling back to environment variables.")
	}

	debug := os.Getenv("DEBUG_MODE") == "true" || os.Getenv("KRAKEN_TEST_MODE") == "true"

	apiKey := os.Getenv("KRAKEN_API_KEY")
	apiSecret := os.Getenv("KRAKEN_API_SECRET")

	var k *kraken.Kraken
	if apiKey == "" || apiSecret == "" {
		if apiKey == "" {
			log.Println("WARNING: KRAKEN_API_KEY is not set")
		}
		if apiSecret == "" {
			log.Println("WARNING: KRAKEN_API_SECRET is not set")
		}
		log.Println("Kraken integration disabled — /api/positions and order execution will not work")
	} else {
		var err error
		k, err = kraken.NewClient(apiKey, apiSecret)
		if err != nil {
			log.Fatalf("Failed to initialize Kraken client: %v", err)
		}
		if debug {
			log.Printf("Kraken client initialized (KRAKEN_TEST_MODE=%s)", os.Getenv("KRAKEN_TEST_MODE"))
		} else {
			log.Println("Kraken client initialized.")
		}
	}

	db, err := database.InitDB("./tvwh2k.db")
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}

	h := handler.NewWebhookHandler(k, db)
	http.HandleFunc("/webhooks", h.ServeHTTP)
	http.HandleFunc("/api/signals", h.HandleGetSignals)
	http.HandleFunc("/api/trades", h.HandleGetTrades)
	http.HandleFunc("/api/positions", h.HandleGetPositions)

	log.Println("Starting server on :8081...")
	if err := http.ListenAndServe(":8081", nil); err != nil {
		log.Fatal(err)
	}
}
