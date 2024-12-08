package main

import (
	"fmt"
	"log"
	"net/http"
	"tvwh2k/handler"
	"tvwh2k/kraken"
)

func main() {
	h := handler.NewWebhookHandler()
	k := kraken.NewClient()
	http.HandleFunc("/webhooks", h.ServeHTTP)

	order := kraken.Order{
		Nonce:     3,
		OrderType: "buy",
		Type:      "limit",
		Volume:    "0.5",
		Pair:      "BTCUSDT",
		Price:     "90000",
		ClOrdID:   "123456789",
		Test:      true,
	}

	k.AddOrder(order)

	fmt.Println("Starting server...")
	if err := http.ListenAndServe(":8081", nil); err != nil {
		log.Fatal(err)
	}
}
