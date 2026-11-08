// Package api holds the JSON bodies exchanged by the server and the client.
package api

import "time"

const TokenHeader = "X-API-Token"

type Challenge struct {
	ID         string    `json:"id" example:"0d5e4bd2-2c32-4f3e-9a7a-2f6a0e1c9b11"`
	Difficulty int       `json:"difficulty"`
	NotBefore  time.Time `json:"not_before"`
	ExpiresAt  time.Time `json:"expires_at"`
}

type RedeemRequest struct {
	Stamp string `json:"stamp"`
}

type Token struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

type Price struct {
	Currency      string    `json:"currency" example:"USD"`
	Price         string    `json:"price" example:"312.45"`
	UpdatedAt     time.Time `json:"updated_at"`
	NextRequestAt time.Time `json:"next_request_at"`
}

type Error struct {
	Message string     `json:"error" example:"rate limited"`
	RetryAt *time.Time `json:"retry_at,omitempty"`
}
