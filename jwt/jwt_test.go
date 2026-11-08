package jwt

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"math"
	"testing"
	"time"
)

type claims struct {
	ID string `json:"id"`
}

var (
	now    = time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	secret = []byte("0123456789abcdef0123456789abcdef")
)

// forge re-signs an arbitrary payload with key, bypassing Sign.
func forge(key []byte, payload string) string {
	s := New[claims](key, 0)
	raw, _ := json.Marshal(envelope{Payload: json.RawMessage(payload), Signature: s.mac([]byte(payload))})
	return base64.RawURLEncoding.EncodeToString(raw)
}

func encode(v string) string { return base64.RawURLEncoding.EncodeToString([]byte(v)) }

func TestSign(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input struct {
				Validity time.Duration
				Data     claims
				Now      time.Time
			}
			Expect struct {
				Expires time.Time
				Payload string
			}
			Test struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{
				Name:   "one hour",
				Input:  Input{time.Hour, claims{"abc"}, now},
				Expect: Expect{now.Add(time.Hour), `{"exp":"2026-11-07T13:00:00Z","data":{"id":"abc"}}`},
			},
			{
				Name:   "three months",
				Input:  Input{2160 * time.Hour, claims{"abc"}, now},
				Expect: Expect{now.Add(2160 * time.Hour), `{"exp":"2027-02-05T12:00:00Z","data":{"id":"abc"}}`},
			},
			{
				Name:   "expiry drops sub-seconds",
				Input:  Input{time.Minute, claims{"x"}, now.Add(999 * time.Millisecond)},
				Expect: Expect{now.Add(time.Minute), `{"exp":"2026-11-07T12:01:00Z","data":{"id":"x"}}`},
			},
			{
				Name:   "expiry is stored in utc",
				Input:  Input{time.Hour, claims{""}, now.In(time.FixedZone("ART", -3*3600))},
				Expect: Expect{now.Add(time.Hour), `{"exp":"2026-11-07T13:00:00Z","data":{"id":""}}`},
			},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				s := New[claims](secret, test.Input.Validity)
				token, p, err := s.Sign(test.Input.Data, test.Input.Now)
				if err != nil {
					t.Fatal(err)
				}
				if !p.Expires.Equal(test.Expect.Expires) || p.Data != test.Input.Data {
					t.Fatalf("payload %+v", p)
				}
				raw, err := base64.RawURLEncoding.DecodeString(token)
				if err != nil {
					t.Fatalf("token is not raw url base64: %v", err)
				}
				var env envelope
				if err := json.Unmarshal(raw, &env); err != nil {
					t.Fatal(err)
				}
				if string(env.Payload) != test.Expect.Payload {
					t.Fatalf("payload %s\nwant    %s", env.Payload, test.Expect.Payload)
				}
				if token != forge(secret, test.Expect.Payload) {
					t.Fatal("signature does not match HMAC-SHA256 over the payload")
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input  struct{ Data float64 }
			Expect struct{ Err bool }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "nan is not json", Input: Input{math.NaN()}, Expect: Expect{true}},
			{Name: "infinity is not json", Input: Input{math.Inf(1)}, Expect: Expect{true}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				token, _, err := New[float64](secret, time.Hour).Sign(test.Input.Data, now)
				if (err != nil) != test.Expect.Err || token != "" {
					t.Fatalf("got %q, %v", token, err)
				}
			})
		}
	})
}

func TestVerify(t *testing.T) {
	signer := New[claims](secret, time.Hour)
	token, _, err := signer.Sign(claims{"abc"}, now)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("Succeed", func(t *testing.T) {
		type (
			Input struct {
				Signer *Signer[claims]
				Token  string
				Now    time.Time
			}
			Expect struct {
				ID      string
				Expires time.Time
			}
			Test struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "right after signing", Input: Input{signer, token, now}, Expect: Expect{"abc", now.Add(time.Hour)}},
			{Name: "last second", Input: Input{signer, token, now.Add(time.Hour - time.Nanosecond)}, Expect: Expect{"abc", now.Add(time.Hour)}},
			{Name: "longer validity accepts it", Input: Input{New[claims](secret, 2*time.Hour), token, now}, Expect: Expect{"abc", now.Add(time.Hour)}},
			{Name: "signed by another instance", Input: Input{New[claims](append([]byte(nil), secret...), time.Hour), token, now.Add(time.Minute)}, Expect: Expect{"abc", now.Add(time.Hour)}},
			{Name: "unknown fields ignored", Input: Input{signer, forge(secret, `{"exp":"2026-11-07T12:30:00Z","data":{"id":"z","admin":true},"x":1}`), now}, Expect: Expect{"z", now.Add(30 * time.Minute)}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				p, err := test.Input.Signer.Verify(test.Input.Token, test.Input.Now)
				if err != nil {
					t.Fatal(err)
				}
				if p.Data.ID != test.Expect.ID || !p.Expires.Equal(test.Expect.Expires) {
					t.Fatalf("got %+v", p)
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input struct {
				Signer *Signer[claims]
				Token  string
				Now    time.Time
			}
			Expect struct{ Err error }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		raw, _ := base64.RawURLEncoding.DecodeString(token)
		var env envelope
		json.Unmarshal(raw, &env)
		swapped := func(payload string) string {
			e := env
			e.Payload = json.RawMessage(payload)
			out, _ := json.Marshal(e)
			return base64.RawURLEncoding.EncodeToString(out)
		}
		// Same JSON value with extra spaces; json.Marshal would compact it, so build it by hand.
		spaced := encode(`{"payload":{"exp": "2026-11-07T13:00:00Z", "data": {"id": "abc"}},"signature":"` +
			base64.StdEncoding.EncodeToString(env.Signature) + `"}`)
		tests := []Test{
			{Name: "expired", Input: Input{signer, token, now.Add(time.Hour)}, Expect: Expect{ErrExpired}},
			{Name: "long expired", Input: Input{signer, token, now.Add(365 * 24 * time.Hour)}, Expect: Expect{ErrExpired}},
			{Name: "validity cut after signing", Input: Input{New[claims](secret, time.Minute), token, now}, Expect: Expect{ErrExpired}},
			{Name: "expiry too far ahead", Input: Input{signer, forge(secret, `{"exp":"2099-01-01T00:00:00Z","data":{"id":"abc"}}`), now}, Expect: Expect{ErrExpired}},
			{Name: "zero expiry", Input: Input{signer, forge(secret, `{"data":{"id":"abc"}}`), now}, Expect: Expect{ErrExpired}},
			{Name: "wrong secret", Input: Input{New[claims]([]byte("fedcba9876543210fedcba9876543210"), time.Hour), token, now}, Expect: Expect{ErrSignature}},
			{Name: "payload swapped", Input: Input{signer, swapped(`{"exp":"2099-01-01T00:00:00Z","data":{"id":"abc"}}`), now}, Expect: Expect{ErrSignature}},
			{Name: "payload reformatted", Input: Input{signer, spaced, now}, Expect: Expect{ErrSignature}},
			{Name: "signature missing", Input: Input{signer, encode(`{"payload":{"exp":"2026-11-07T13:00:00Z","data":{"id":"abc"}}}`), now}, Expect: Expect{ErrSignature}},
			{Name: "padded base64", Input: Input{signer, base64.URLEncoding.EncodeToString(raw), now}, Expect: Expect{ErrMalformed}},
			{Name: "standard base64 alphabet", Input: Input{signer, "+/" + token, now}, Expect: Expect{ErrMalformed}},
			{Name: "garbage", Input: Input{signer, "not-a-token!", now}, Expect: Expect{ErrMalformed}},
			{Name: "empty", Input: Input{signer, "", now}, Expect: Expect{ErrMalformed}},
			{Name: "not json", Input: Input{signer, encode("hello"), now}, Expect: Expect{ErrMalformed}},
			{Name: "no payload", Input: Input{signer, encode(`{"signature":"AAAA"}`), now}, Expect: Expect{ErrMalformed}},
			{Name: "signed payload of the wrong shape", Input: Input{signer, forge(secret, `{"exp":"tomorrow","data":{"id":"abc"}}`), now}, Expect: Expect{ErrMalformed}},
			{Name: "signed payload not an object", Input: Input{signer, forge(secret, `[1,2,3]`), now}, Expect: Expect{ErrMalformed}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				if _, err := test.Input.Signer.Verify(test.Input.Token, test.Input.Now); !errors.Is(err, test.Expect.Err) {
					t.Fatalf("got %v, want %v", err, test.Expect.Err)
				}
			})
		}
	})
}
