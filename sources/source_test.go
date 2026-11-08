package sources

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestNum(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input  struct{ JSON string }
			Expect struct{ Text num }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "quoted", Input: Input{`"312.45"`}, Expect: Expect{"312.45"}},
			{Name: "bare number", Input: Input{`312.45`}, Expect: Expect{"312.45"}},
			{Name: "exponent", Input: Input{`3.05e-3`}, Expect: Expect{"3.05e-3"}},
			{Name: "null", Input: Input{`null`}, Expect: Expect{""}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				var n num
				if err := json.Unmarshal([]byte(test.Input.JSON), &n); err != nil {
					t.Fatal(err)
				}
				if n != test.Expect.Text {
					t.Fatalf("got %q", n)
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input struct {
				Text  num
				Scale int
			}
			Expect struct{ OK bool }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "empty", Input: Input{"", 2}, Expect: Expect{false}},
			{Name: "zero", Input: Input{"0", 2}, Expect: Expect{false}},
			{Name: "rounds to zero", Input: Input{"0.001", 2}, Expect: Expect{false}},
			{Name: "not a number", Input: Input{"n/a", 2}, Expect: Expect{false}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				if v, ok := test.Input.Text.fixed(test.Input.Scale); ok != test.Expect.OK {
					t.Fatalf("accepted as %d", v)
				}
			})
		}
	})
}

func TestQuote(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input struct {
				Scale          int
				Bid, Ask, Last num
			}
			Expect struct{ Price uint64 }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "midpoint", Input: Input{2, "299", "301", "250"}, Expect: Expect{30000}},
			{Name: "odd midpoint rounds down", Input: Input{2, "300.00", "300.01", ""}, Expect: Expect{30000}},
			{Name: "both odd", Input: Input{2, "300.01", "300.03", ""}, Expect: Expect{30002}},
			{Name: "no bid uses last", Input: Input{2, "", "301", "300.5"}, Expect: Expect{30050}},
			{Name: "no ask uses last", Input: Input{2, "299", "0", "300.5"}, Expect: Expect{30050}},
			{Name: "only last", Input: Input{8, "", "", "0.00305"}, Expect: Expect{305000}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				got, ok := quote(test.Input.Scale, test.Input.Bid, test.Input.Ask, test.Input.Last)
				if !ok || got != test.Expect.Price {
					t.Fatalf("got %d, %v", got, ok)
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input struct {
				Scale          int
				Bid, Ask, Last num
			}
			Expect struct{ OK bool }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "nothing", Input: Input{2, "", "", ""}, Expect: Expect{false}},
			{Name: "zeros", Input: Input{2, "0", "0", "0"}, Expect: Expect{false}},
			{Name: "garbage last", Input: Input{2, "", "", "x"}, Expect: Expect{false}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				if got, ok := quote(test.Input.Scale, test.Input.Bid, test.Input.Ask, test.Input.Last); ok != test.Expect.OK {
					t.Fatalf("got %d", got)
				}
			})
		}
	})
}

func TestAdd(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input struct {
				Base, Quote string
				Last        num
			}
			Expect struct{ Pair string }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "xmr fiat", Input: Input{"XMR", "USD", "300"}, Expect: Expect{"XMR/USD=300.00"}},
			{Name: "xmr btc", Input: Input{"XMR", "BTC", "0.003"}, Expect: Expect{"XMR/BTC=0.00300000"}},
			{Name: "btc fiat", Input: Input{"BTC", "EUR", "90000"}, Expect: Expect{"BTC/EUR=90000.00"}},
			{Name: "xbt is bitcoin", Input: Input{"XBT", "EUR", "90000"}, Expect: Expect{"BTC/EUR=90000.00"}},
			{Name: "xbt quote", Input: Input{"XMR", "XBT", "0.003"}, Expect: Expect{"XMR/BTC=0.00300000"}},
			{Name: "lower case", Input: Input{"btc", "krw", "1"}, Expect: Expect{"BTC/KRW=1.00"}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				rates := add(nil, "src", test.Input.Base, test.Input.Quote, "", "", test.Input.Last, zeroTime)
				if got := pairs(rates); len(got) != 1 || got[0] != test.Expect.Pair {
					t.Fatalf("got %v", got)
				}
				if rates[0].Time.IsZero() || rates[0].Source != "src" {
					t.Fatalf("time or source missing: %+v", rates[0])
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input struct {
				Base, Quote string
				Last        num
			}
			Expect struct{ OK bool }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "altcoin base", Input: Input{"ETH", "USD", "3000"}, Expect: Expect{false}},
			{Name: "stablecoin quote", Input: Input{"XMR", "USDT", "300"}, Expect: Expect{false}},
			{Name: "btc quoted in btc", Input: Input{"BTC", "BTC", "1"}, Expect: Expect{false}},
			{Name: "btc quoted in xmr", Input: Input{"BTC", "XMR", "330"}, Expect: Expect{false}},
			{Name: "zero price", Input: Input{"XMR", "USD", "0"}, Expect: Expect{false}},
			{Name: "unparsable price", Input: Input{"XMR", "USD", "?"}, Expect: Expect{false}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				if rates := add(nil, "src", test.Input.Base, test.Input.Quote, "", "", test.Input.Last, zeroTime); (len(rates) == 1) != test.Expect.OK {
					t.Fatalf("kept %v", pairs(rates))
				}
			})
		}
	})
}

func TestGetJSON(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input struct {
				Reply  reply
				Header http.Header
			}
			Expect struct{ Value string }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "plain", Input: Input{Reply: ok(`{"v":"a"}`)}, Expect: Expect{"a"}},
			{Name: "created", Input: Input{Reply: reply{http.StatusCreated, `{"v":"b"}`}}, Expect: Expect{"b"}},
			{Name: "extra header", Input: Input{Reply: ok(`{"v":"c"}`), Header: http.Header{"X-Key": {"k"}}}, Expect: Expect{"c"}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if want := test.Input.Header.Get("X-Key"); r.Header.Get("X-Key") != want {
						t.Errorf("header X-Key = %q", r.Header.Get("X-Key"))
					}
					w.WriteHeader(test.Input.Reply.Status)
					w.Write([]byte(test.Input.Reply.Body))
				}))
				defer srv.Close()
				var out struct{ V string }
				if err := getJSON(t.Context(), nil, srv.URL, test.Input.Header, &out); err != nil {
					t.Fatal(err)
				}
				if out.V != test.Expect.Value {
					t.Fatalf("got %q", out.V)
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input struct {
				Reply    reply
				Canceled bool
			}
			Expect struct{ ErrContains string }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "server error", Input: Input{Reply: reply{500, "boom"}}, Expect: Expect{"500 Internal Server Error: boom"}},
			{Name: "blocked region", Input: Input{Reply: reply{451, ""}}, Expect: Expect{"451"}},
			{Name: "not json", Input: Input{Reply: ok("<html>")}, Expect: Expect{"invalid character"}},
			{Name: "too large", Input: Input{Reply: ok(`{"v":"` + strings.Repeat("x", maxBody) + `"}`)}, Expect: Expect{"unexpected EOF"}},
			{Name: "canceled", Input: Input{Reply: ok(`{}`), Canceled: true}, Expect: Expect{"context canceled"}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(test.Input.Reply.Status)
					w.Write([]byte(test.Input.Reply.Body))
				}))
				defer srv.Close()
				ctx, cancel := context.WithCancel(t.Context())
				if test.Input.Canceled {
					cancel()
				}
				defer cancel()
				var out struct{ V string }
				err := getJSON(ctx, nil, srv.URL, nil, &out)
				if err == nil || !strings.Contains(err.Error(), test.Expect.ErrContains) {
					t.Fatalf("got %v, want it to contain %q", err, test.Expect.ErrContains)
				}
			})
		}
	})
}

type stubSource struct {
	name  string
	rates []Rate
	err   error
	delay time.Duration
	blue  bool
}

func (s stubSource) Name() string   { return s.name }
func (s stubSource) BlueRate() bool { return s.blue }
func (s stubSource) Fetch(ctx context.Context) ([]Rate, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(s.delay):
	}
	return slices.Clone(s.rates), s.err
}

func TestFetchAll(t *testing.T) {
	rate := Rate{Base: "XMR", Quote: "USD", Price: 30000, Time: time.Now(), Source: "x"}

	t.Run("Succeed", func(t *testing.T) {
		type (
			Input  struct{ Source stubSource }
			Expect struct {
				Rates int
				Blue  bool
			}
			Test struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "plain", Input: Input{stubSource{name: "a", rates: []Rate{rate, rate}}}, Expect: Expect{Rates: 2}},
			{Name: "blue source marks its rates", Input: Input{stubSource{name: "b", rates: []Rate{rate}, blue: true}}, Expect: Expect{Rates: 1, Blue: true}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				results := FetchAll(t.Context(), time.Second, test.Input.Source)
				r := results[0]
				if r.Err != nil || r.Source != test.Input.Source.name || len(r.Rates) != test.Expect.Rates {
					t.Fatalf("%+v", r)
				}
				for _, got := range r.Rates {
					if got.Blue != test.Expect.Blue {
						t.Fatalf("blue = %v", got.Blue)
					}
				}
				if n := len(AllRates(results)); n != test.Expect.Rates {
					t.Fatalf("AllRates = %d", n)
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input  struct{ Source stubSource }
			Expect struct{ ErrContains string }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "source error", Input: Input{stubSource{name: "a", err: errors.New("down")}}, Expect: Expect{"down"}},
			{Name: "nothing usable", Input: Input{stubSource{name: "b"}}, Expect: Expect{"no usable rates"}},
			{Name: "timeout", Input: Input{stubSource{name: "c", rates: []Rate{rate}, delay: time.Minute}}, Expect: Expect{"deadline exceeded"}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				results := FetchAll(t.Context(), 50*time.Millisecond, test.Input.Source)
				if err := results[0].Err; err == nil || !strings.Contains(err.Error(), test.Expect.ErrContains) {
					t.Fatalf("got %v", err)
				}
				if n := len(AllRates(results)); n != 0 {
					t.Fatalf("AllRates = %d", n)
				}
			})
		}
	})
}
