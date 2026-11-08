// Package jwt signs small JSON tokens: base64url({"payload": ..., "signature": ...})
// where the signature is HMAC-SHA256 over the exact payload bytes.
package jwt

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrMalformed = errors.New("jwt: malformed token")
	ErrSignature = errors.New("jwt: invalid signature")
	ErrExpired   = errors.New("jwt: expired")
)

type Payload[T any] struct {
	Expires time.Time `json:"exp"`
	Data    T         `json:"data"`
}

type envelope struct {
	Payload   json.RawMessage `json:"payload"`
	Signature []byte          `json:"signature"`
}

type Signer[T any] struct {
	secret   []byte
	validity time.Duration
}

// New returns a signer whose tokens expire validity after signing.
func New[T any](secret []byte, validity time.Duration) *Signer[T] {
	return &Signer[T]{secret: secret, validity: validity}
}

func (s *Signer[T]) mac(payload []byte) []byte {
	h := hmac.New(sha256.New, s.secret)
	h.Write(payload)
	return h.Sum(nil)
}

func (s *Signer[T]) Sign(data T, now time.Time) (string, Payload[T], error) {
	p := Payload[T]{Expires: now.Add(s.validity).UTC().Truncate(time.Second), Data: data}
	raw, err := json.Marshal(p)
	if err != nil {
		return "", p, err
	}
	token, err := json.Marshal(envelope{Payload: raw, Signature: s.mac(raw)})
	if err != nil {
		return "", p, err
	}
	return base64.RawURLEncoding.EncodeToString(token), p, nil
}

// Verify checks the signature and expiry. A token whose expiry lies further
// ahead than the current validity is rejected too.
func (s *Signer[T]) Verify(token string, now time.Time) (Payload[T], error) {
	var p Payload[T]
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return p, ErrMalformed
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil || len(env.Payload) == 0 {
		return p, ErrMalformed
	}
	if !hmac.Equal(env.Signature, s.mac(env.Payload)) {
		return p, ErrSignature
	}
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		return p, ErrMalformed
	}
	if !now.Before(p.Expires) || p.Expires.After(now.Add(s.validity)) {
		return p, ErrExpired
	}
	return p, nil
}
