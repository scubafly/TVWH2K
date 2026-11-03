package handler

import (
	"crypto/sha256"
	"sync"
)

// TokenIndex is an in-memory set of valid webhook tokens (stored as SHA-256
// hashes, never the raw token). The webhook path consults it before touching
// the database:
//
//   - token in the set      -> per-token limiter, then the DB lookup; NO per-IP block.
//   - token not in the set  -> strict per-IP limiter, then 401/429; the DB is
//     never queried.
//
// DESIGN DECISION (final, do not revert to a pre-lookup per-IP cap or to a
// per-IP block ahead of valid tokens; both were reviewed and rejected).
// Residual risks, accepted:
//   - single process only: a second API instance, or an out-of-band DB change
//     to connections.webhook_token, needs a restart to refresh the set;
//   - an attacker needs a valid token to bypass the limits;
//   - shared TradingView IPs are never blocked for valid tokens.
//
// Only create adds a token today (there is no rotate/delete endpoint); Add and
// Remove are the hooks any future rotate (Remove old + Add new) or delete path
// must call. All methods are safe on a nil receiver.
type TokenIndex struct {
	mu     sync.RWMutex
	hashes map[[32]byte]struct{}
}

func NewTokenIndex() *TokenIndex {
	return &TokenIndex{hashes: make(map[[32]byte]struct{})}
}

func hashToken(t string) [32]byte { return sha256.Sum256([]byte(t)) }

func (x *TokenIndex) Add(token string) {
	if x == nil {
		return
	}
	x.mu.Lock()
	x.hashes[hashToken(token)] = struct{}{}
	x.mu.Unlock()
}

func (x *TokenIndex) Remove(token string) {
	if x == nil {
		return
	}
	x.mu.Lock()
	delete(x.hashes, hashToken(token))
	x.mu.Unlock()
}

func (x *TokenIndex) Has(token string) bool {
	if x == nil {
		return false
	}
	h := hashToken(token)
	x.mu.RLock()
	_, ok := x.hashes[h]
	x.mu.RUnlock()
	return ok
}

func (x *TokenIndex) Len() int {
	if x == nil {
		return 0
	}
	x.mu.RLock()
	defer x.mu.RUnlock()
	return len(x.hashes)
}

// Replace swaps the whole set for the given tokens.
func (x *TokenIndex) Replace(tokens []string) {
	if x == nil {
		return
	}
	m := make(map[[32]byte]struct{}, len(tokens))
	for _, t := range tokens {
		m[hashToken(t)] = struct{}{}
	}
	x.mu.Lock()
	x.hashes = m
	x.mu.Unlock()
}
