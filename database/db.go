package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type DB struct {
	*sql.DB
}

// InitDB opens a connection pool to Postgres (Supabase). connString is a
// standard "postgres://user:pass@host:port/dbname?sslmode=require" URL.
// Run migrations/0001_init.sql against the target database before starting
// the app -- this function does not create tables.
func InitDB(connString string) (*DB, error) {
	db, err := sql.Open("pgx", connString)
	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)

	return &DB{db}, nil
}

type Connection struct {
	ID                        int64
	UserID                    string
	Exchange                  string
	KrakenAPIKeyEncrypted     []byte
	KrakenAPISecretEncrypted  []byte
	WebhookToken              string
	TestMode                  bool
	TelegramBotTokenEncrypted []byte
	TelegramChatID            sql.NullInt64
	CreatedAt                 time.Time
}

func (db *DB) CreateConnection(userID string, apiKeyEnc, apiSecretEnc []byte, webhookToken string, testMode bool) (*Connection, error) {
	var c Connection
	err := db.QueryRow(
		`INSERT INTO connections (user_id, kraken_api_key_encrypted, kraken_api_secret_encrypted, webhook_token, test_mode)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, user_id, exchange, kraken_api_key_encrypted, kraken_api_secret_encrypted, webhook_token, test_mode, telegram_chat_id, created_at`,
		userID, apiKeyEnc, apiSecretEnc, webhookToken, testMode,
	).Scan(&c.ID, &c.UserID, &c.Exchange, &c.KrakenAPIKeyEncrypted, &c.KrakenAPISecretEncrypted, &c.WebhookToken, &c.TestMode, &c.TelegramChatID, &c.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection: %w", err)
	}
	return &c, nil
}

func (db *DB) GetConnectionByWebhookToken(token string) (*Connection, error) {
	var c Connection
	err := db.QueryRow(
		`SELECT id, user_id, exchange, kraken_api_key_encrypted, kraken_api_secret_encrypted, webhook_token, test_mode, telegram_bot_token_encrypted, telegram_chat_id, created_at
		 FROM connections WHERE webhook_token = $1`,
		token,
	).Scan(&c.ID, &c.UserID, &c.Exchange, &c.KrakenAPIKeyEncrypted, &c.KrakenAPISecretEncrypted, &c.WebhookToken, &c.TestMode, &c.TelegramBotTokenEncrypted, &c.TelegramChatID, &c.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// GetConnectionByIDForUser returns the connection only if it belongs to userID,
// so callers can scope /api/signals and /api/trades to the authenticated caller.
func (db *DB) GetConnectionByIDForUser(id int64, userID string) (*Connection, error) {
	var c Connection
	err := db.QueryRow(
		`SELECT id, user_id, exchange, kraken_api_key_encrypted, kraken_api_secret_encrypted, webhook_token, test_mode, telegram_bot_token_encrypted, telegram_chat_id, created_at
		 FROM connections WHERE id = $1 AND user_id = $2`,
		id, userID,
	).Scan(&c.ID, &c.UserID, &c.Exchange, &c.KrakenAPIKeyEncrypted, &c.KrakenAPISecretEncrypted, &c.WebhookToken, &c.TestMode, &c.TelegramBotTokenEncrypted, &c.TelegramChatID, &c.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (db *DB) GetConnectionsByUser(userID string) ([]Connection, error) {
	rows, err := db.Query(
		`SELECT id, user_id, exchange, kraken_api_key_encrypted, kraken_api_secret_encrypted, webhook_token, test_mode, telegram_bot_token_encrypted, telegram_chat_id, created_at
		 FROM connections WHERE user_id = $1 ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var connections []Connection
	for rows.Next() {
		var c Connection
		if err := rows.Scan(&c.ID, &c.UserID, &c.Exchange, &c.KrakenAPIKeyEncrypted, &c.KrakenAPISecretEncrypted, &c.WebhookToken, &c.TestMode, &c.TelegramBotTokenEncrypted, &c.TelegramChatID, &c.CreatedAt); err != nil {
			return nil, err
		}
		connections = append(connections, c)
	}
	return connections, nil
}

type Signal struct {
	ID           int64     `json:"id"`
	ConnectionID int64     `json:"connection_id"`
	ReceivedAt   time.Time `json:"received_at"`
	Pair         string    `json:"pair"`
	Type         string    `json:"type"`
	Payload      string    `json:"payload"`
}

func (db *DB) SaveSignal(connectionID int64, pair, action string, payload interface{}) (int64, error) {
	payloadBytes, _ := json.Marshal(payload)
	var id int64
	err := db.QueryRow(
		`INSERT INTO signals (connection_id, pair, type, payload) VALUES ($1, $2, $3, $4) RETURNING id`,
		connectionID, pair, action, payloadBytes,
	).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

func (db *DB) GetRecentSignals(connectionID int64, limit int) ([]Signal, error) {
	rows, err := db.Query(
		`SELECT id, connection_id, received_at, pair, type, payload FROM signals WHERE connection_id = $1 ORDER BY received_at DESC LIMIT $2`,
		connectionID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var signals []Signal
	for rows.Next() {
		var s Signal
		if err := rows.Scan(&s.ID, &s.ConnectionID, &s.ReceivedAt, &s.Pair, &s.Type, &s.Payload); err != nil {
			return nil, err
		}
		signals = append(signals, s)
	}
	return signals, nil
}

type Trade struct {
	ID           int64     `json:"id"`
	SignalID     int64     `json:"signal_id"`
	ConnectionID int64     `json:"connection_id"`
	Pair         string    `json:"pair"`
	Type         string    `json:"type"`
	OrderType    string    `json:"ordertype"`
	Volume       string    `json:"volume"`
	Price        string    `json:"price"`
	TxID         string    `json:"txid"`
	CreatedAt    time.Time `json:"created_at"`
	Status       string    `json:"status"`
	PnL          float64   `json:"pnl"`
}

func (db *DB) SaveTrade(signalID, connectionID int64, pair, action, orderType, volume, price, txid string) error {
	_, err := db.Exec(
		`INSERT INTO trades (signal_id, connection_id, pair, type, ordertype, volume, price, txid)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		signalID, connectionID, pair, action, orderType, volume, price, txid,
	)
	return err
}

func (db *DB) GetRecentTrades(connectionID int64, limit int) ([]Trade, error) {
	rows, err := db.Query(
		`SELECT id, signal_id, connection_id, pair, type, ordertype, volume, price, txid, created_at, status, pnl
		 FROM trades WHERE connection_id = $1 ORDER BY created_at DESC LIMIT $2`,
		connectionID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var trades []Trade
	for rows.Next() {
		var t Trade
		if err := rows.Scan(&t.ID, &t.SignalID, &t.ConnectionID, &t.Pair, &t.Type, &t.OrderType, &t.Volume, &t.Price, &t.TxID, &t.CreatedAt, &t.Status, &t.PnL); err != nil {
			return nil, err
		}
		trades = append(trades, t)
	}
	return trades, nil
}
