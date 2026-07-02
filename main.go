package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"tvwh2k/auth"
	"tvwh2k/database"
	"tvwh2k/handler"
	"tvwh2k/security"
)

func main() {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL not set (postgres connection string to your Supabase project)")
	}
	db, err := database.InitDB(dbURL)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	fmt.Println("Connected to database.")

	encryptor, err := security.NewEncryptorFromEnv()
	if err != nil {
		log.Fatalf("Failed to initialize encryption: %v", err)
	}

	verifier, err := auth.NewVerifierFromEnv()
	if err != nil {
		log.Fatalf("Failed to initialize auth verifier: %v", err)
	}

	apiBaseURL := os.Getenv("API_BASE_URL")
	if apiBaseURL == "" {
		log.Fatal("API_BASE_URL not set (public base URL of this service, used to build webhook URLs)")
	}

	webhookHandler := handler.NewWebhookHandler(db, encryptor)
	connectionsHandler := handler.NewConnectionsHandler(db, encryptor, apiBaseURL)

	mux := http.NewServeMux()

	// Public: TradingView calls this directly, the per-connection token is the auth.
	mux.HandleFunc("POST /webhooks/{token}", webhookHandler.ServeHTTP)

	// Authenticated (Supabase JWT) endpoints for the frontend.
	mux.HandleFunc("POST /api/connections", verifier.Middleware(connectionsHandler.Create))
	mux.HandleFunc("GET /api/connections", verifier.Middleware(connectionsHandler.List))
	mux.HandleFunc("POST /api/connections/{id}/test", verifier.Middleware(connectionsHandler.Test))
	mux.HandleFunc("GET /api/signals", verifier.Middleware(webhookHandler.HandleGetSignals))
	mux.HandleFunc("GET /api/trades", verifier.Middleware(webhookHandler.HandleGetTrades))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}
	fmt.Printf("Starting server on :%s...\n", port)
	if err := http.ListenAndServe(":"+port, withCORS(mux)); err != nil {
		log.Fatal(err)
	}
}

// withCORS allows the frontend (a different origin, e.g. tradingbridge.online
// calling api.tradingbridge.online) to call the /api/* routes with a bearer
// token. /webhooks/* doesn't need this -- TradingView calls it server-to-server.
func withCORS(next http.Handler) http.Handler {
	frontendOrigin := os.Getenv("FRONTEND_ORIGIN")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if frontendOrigin != "" {
			w.Header().Set("Access-Control-Allow-Origin", frontendOrigin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
