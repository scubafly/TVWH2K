package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

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

	// Server-wide kill-switch: orders are validate-only (dry-run) unless
	// KRAKEN_TEST_MODE is explicitly set to "false".
	testMode := os.Getenv("KRAKEN_TEST_MODE") != "false"
	if testMode {
		fmt.Println("Test mode enabled: Kraken orders are validate-only (set KRAKEN_TEST_MODE=false to trade live).")
	} else {
		fmt.Println("LIVE trading mode: Kraken orders will be executed for real.")
	}

	webhookHandler := handler.NewWebhookHandler(db, encryptor, testMode)
	connectionsHandler := handler.NewConnectionsHandler(db, encryptor, apiBaseURL, testMode)

	mux := http.NewServeMux()

	// Public: TradingView calls this directly, the per-connection token is the auth.
	mux.HandleFunc("POST /webhooks/{token}", webhookHandler.ServeHTTP)

	// Authenticated (Supabase JWT) endpoints for the frontend.
	mux.HandleFunc("POST /api/connections", verifier.Middleware(connectionsHandler.Create))
	mux.HandleFunc("GET /api/connections", verifier.Middleware(connectionsHandler.List))
	mux.HandleFunc("PATCH /api/connections/{id}", verifier.Middleware(connectionsHandler.Update))
	mux.HandleFunc("POST /api/connections/{id}/test", verifier.Middleware(connectionsHandler.Test))
	mux.HandleFunc("GET /api/signals", verifier.Middleware(webhookHandler.HandleGetSignals))
	mux.HandleFunc("GET /api/trades", verifier.Middleware(webhookHandler.HandleGetTrades))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: withCORS(mux),
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		fmt.Printf("Starting server on :%s...\n", port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()

	<-ctx.Done()
	fmt.Println("Shutting down: draining in-flight requests and queued signals...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP shutdown error: %v", err)
	}
	// After the server stops accepting webhooks, let the workers finish the
	// signals that were already acknowledged.
	webhookHandler.Close()
	fmt.Println("Shutdown complete.")
}

// withCORS allows the frontend (a different origin, e.g. tradingbridge.online
// calling api.tradingbridge.online) to call the /api/* routes with a bearer
// token. /webhooks/* doesn't need this -- TradingView calls it server-to-server.
func withCORS(next http.Handler) http.Handler {
	frontendOrigin := os.Getenv("FRONTEND_ORIGIN")
	if frontendOrigin == "" {
		log.Println("WARNING: FRONTEND_ORIGIN not set -- browser requests from the frontend will fail CORS preflight.")
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if frontendOrigin != "" {
			w.Header().Set("Access-Control-Allow-Origin", frontendOrigin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
