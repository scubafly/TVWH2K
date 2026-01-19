package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type DB struct {
	*sql.DB
}

func InitDB(filepath string) (*DB, error) {
	db, err := sql.Open("sqlite3", filepath)
	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		return nil, err
	}

	// Enable WAL mode for better concurrency
	if _, err := db.Exec("PRAGMA journal_mode = WAL;"); err != nil {
		log.Printf("Failed to enable WAL mode: %v", err)
	}

	if err := createTables(db); err != nil {
		return nil, err
	}

	return &DB{db}, nil
}

func createTables(db *sql.DB) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS signals (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			received_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			pair TEXT,
			type TEXT,
			payload TEXT
		);`,
		`CREATE TABLE IF NOT EXISTS trades (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			signal_id INTEGER,
			pair TEXT,
			type TEXT,
			ordertype TEXT,
			volume TEXT,
			price TEXT,
			txid TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			status TEXT DEFAULT 'open',
			pnl REAL DEFAULT 0,
			FOREIGN KEY(signal_id) REFERENCES signals(id)
		);`,
	}

	for _, query := range queries {
		if _, err := db.Exec(query); err != nil {
			return fmt.Errorf("error creating table: %w", err)
		}
	}
	return nil
}

type Signal struct {
	ID         int64
	ReceivedAt time.Time
	Pair       string
	Type       string // buy/sell
	Payload    string
}

func (db *DB) SaveSignal(pair, action string, payload interface{}) (int64, error) {
	payloadBytes, _ := json.Marshal(payload)
	res, err := db.Exec("INSERT INTO signals (pair, type, payload) VALUES (?, ?, ?)", pair, action, string(payloadBytes))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (db *DB) SaveTrade(signalID int64, pair, action, orderType, volume, price, txid string) error {
	_, err := db.Exec(`INSERT INTO trades (signal_id, pair, type, ordertype, volume, price, txid)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		signalID, pair, action, orderType, volume, price, txid)
	return err
}

func (db *DB) GetRecentSignals(limit int) ([]Signal, error) {
	rows, err := db.Query("SELECT id, received_at, pair, type, payload FROM signals ORDER BY received_at DESC LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var signals []Signal
	for rows.Next() {
		var s Signal
		if err := rows.Scan(&s.ID, &s.ReceivedAt, &s.Pair, &s.Type, &s.Payload); err != nil {
			return nil, err
		}
		signals = append(signals, s)
	}
	return signals, nil
}

type Trade struct {
	ID        int64     `json:"id"`
	SignalID  int64     `json:"signal_id"`
	Pair      string    `json:"pair"`
	Type      string    `json:"type"`
	OrderType string    `json:"ordertype"`
	Volume    string    `json:"volume"`
	Price     string    `json:"price"`
	TxID      string    `json:"txid"`
	CreatedAt time.Time `json:"created_at"`
	Status    string    `json:"status"`
	PnL       float64   `json:"pnl"`
}

func (db *DB) GetRecentTrades(limit int) ([]Trade, error) {
	rows, err := db.Query("SELECT id, signal_id, pair, type, ordertype, volume, price, txid, created_at, status, pnl FROM trades ORDER BY created_at DESC LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var trades []Trade
	for rows.Next() {
		var t Trade
		if err := rows.Scan(&t.ID, &t.SignalID, &t.Pair, &t.Type, &t.OrderType, &t.Volume, &t.Price, &t.TxID, &t.CreatedAt, &t.Status, &t.PnL); err != nil {
			return nil, err
		}
		trades = append(trades, t)
	}
	return trades, nil
}
