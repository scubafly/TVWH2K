package main

import (
	"fmt"
	"log"
	"net/http"
	"tvwh2k/handler"
)

func main() {
	h := handler.NewWebhookHandler( /* Parameters */ ) // Replace with actual parameters
	http.HandleFunc("/webhooks", h.ServeHTTP)

	fmt.Println("Starting server...")
	if err := http.ListenAndServe(":8081", nil); err != nil {
		log.Fatal(err)
	}
}
