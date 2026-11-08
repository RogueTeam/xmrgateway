package sources

import (
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
	"testing"
	"time"
)

var aggNow = time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)

// mkRate builds a rate seen age before aggNow, e.g. mkRate("kraken", "XMR/USD", "300.00", 0).
func mkRate(source, pair, price string, age time.Duration) Rate {
	base, quote, _ := strings.Cut(pair, "/")
	v, err := ParseDecimal(price, Scale(quote))
	if err != nil {
		panic(err)
	}
	return Rate{Base: base, Quote: quote, Price: v, Time: aggNow.Add(-age), Source: source}
}

func rawRate(source, pair string, price uint64) Rate {
	r := mkRate(source, pair, "0", 0)
	r.Price = price
	return r
}

func blue(r Rate) Rate {
	r.Blue = true
	return r
}

func TestAggregate(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input struct {
				Rates   []Rate
				Options Options
			}
			Expect struct {
				Prices   map[string]string
				Sources  map[string][]string
				Rejected map[string][]string
				Age      map[string]time.Duration
			}
			Test struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{
				Name: "direct votes are averaged",
				Input: Input{Rates: []Rate{
					mkRate("a", "XMR/USD", "300.00", 0),
					mkRate("b", "XMR/USD", "302.00", 0),
					mkRate("c", "XMR/USD", "301.00", 0),
				}},
				Expect: Expect{
					Prices:  map[string]string{"USD": "301.00"},
					Sources: map[string][]string{"USD": {"a", "b", "c"}},
				},
			},
			{
				Name: "outlier is rejected",
				Input: Input{Rates: []Rate{
					mkRate("a", "XMR/USD", "300.00", 0),
					mkRate("b", "XMR/USD", "301.00", 0),
					mkRate("c", "XMR/USD", "302.00", 0),
					mkRate("evil", "XMR/USD", "900.00", 0),
				}},
				Expect: Expect{
					Prices:   map[string]string{"USD": "301.00"},
					Sources:  map[string][]string{"USD": {"a", "b", "c"}},
					Rejected: map[string][]string{"USD": {"evil"}},
				},
			},
			{
				Name: "btc markets vote through xmr/btc",
				Input: Input{Rates: []Rate{
					mkRate("kraken", "XMR/BTC", "0.00300000", 2*time.Minute),
					mkRate("bitstamp", "BTC/EUR", "90000.00", time.Minute),
				}},
				Expect: Expect{
					Prices:  map[string]string{"BTC": "0.00300000", "EUR": "270.00"},
					Sources: map[string][]string{"EUR": {"bitstamp/btc"}},
					Age:     map[string]time.Duration{"EUR": 2 * time.Minute},
				},
			},
			{
				Name: "direct and derived votes are combined",
				Input: Input{Rates: []Rate{
					mkRate("kraken", "XMR/BTC", "0.00300000", 0),
					mkRate("kraken", "XMR/EUR", "280.00", 0),
					mkRate("bitstamp", "BTC/EUR", "90000.00", 0),
				}},
				Expect: Expect{
					Prices:  map[string]string{"BTC": "0.00300000", "EUR": "275.00"},
					Sources: map[string][]string{"EUR": {"bitstamp/btc", "kraken"}},
				},
			},
			{
				Name: "bad xmr/btc vote does not leak into fiat",
				Input: Input{Rates: []Rate{
					mkRate("a", "XMR/BTC", "0.00300000", 0),
					mkRate("b", "XMR/BTC", "0.00302000", 0),
					mkRate("evil", "XMR/BTC", "0.00500000", 0),
					mkRate("bitstamp", "BTC/USD", "100000.00", 0),
				}},
				Expect: Expect{
					Prices:   map[string]string{"BTC": "0.00301000", "USD": "301.00"},
					Rejected: map[string][]string{"BTC": {"evil"}, "USD": nil},
				},
			},
			{
				Name: "half a unit rounds up",
				Input: Input{Rates: []Rate{
					mkRate("a", "XMR/USD", "1.00", 0),
					mkRate("b", "XMR/USD", "1.01", 0),
				}},
				Expect: Expect{Prices: map[string]string{"USD": "1.01"}},
			},
			{
				Name: "a third of a unit rounds down",
				Input: Input{Rates: []Rate{
					mkRate("a", "XMR/USD", "1.00", 0),
					mkRate("b", "XMR/USD", "1.00", 0),
					mkRate("c", "XMR/USD", "1.01", 0),
				}},
				Expect: Expect{Prices: map[string]string{"USD": "1.00"}},
			},
			{
				Name: "stale and excluded rates are skipped",
				Input: Input{Rates: []Rate{
					mkRate("a", "XMR/USD", "300.00", 0),
					mkRate("old", "XMR/USD", "100.00", 11*time.Minute),
					mkRate("binance:spot", "XMR/USD", "500.00", 0),
					mkRate("kraken", "XMR/AED", "1100.00", 0),
					mkRate("x", "XMR/LBP", "26850000.00", 0),
				}},
				Expect: Expect{
					Prices:   map[string]string{"USD": "300.00"},
					Sources:  map[string][]string{"USD": {"a"}},
					Rejected: map[string][]string{"USD": nil},
				},
			},
			{
				Name: "price time is the newest accepted vote",
				Input: Input{Rates: []Rate{
					mkRate("a", "XMR/USD", "300.00", 3*time.Minute),
					mkRate("b", "XMR/USD", "301.00", time.Minute),
					mkRate("evil", "XMR/USD", "900.00", 0),
				}},
				Expect: Expect{
					Prices: map[string]string{"USD": "300.50"},
					Age:    map[string]time.Duration{"USD": time.Minute},
				},
			},
			{
				Name: "custom limits",
				Input: Input{
					Rates: []Rate{
						mkRate("x", "XMR/LBP", "26850000.00", 30*time.Minute),
						mkRate("a", "XMR/USD", "300.00", 0),
						mkRate("b", "XMR/USD", "500.00", 0),
					},
					Options: Options{MaxAge: time.Hour, MaxDeviationBps: 10000, ExcludeQuotes: []string{}},
				},
				Expect: Expect{Prices: map[string]string{"LBP": "26850000.00", "USD": "400.00"}},
			},
			{
				Name: "official ars is lifted to the blue rate",
				Input: Input{
					Rates: []Rate{
						mkRate("kraken", "XMR/BTC", "0.00300000", 0),
						mkRate("official", "BTC/ARS", "100000000.00", 0),
						blue(mkRate("criptoya:bitso", "BTC/ARS", "130000000.00", 0)),
					},
					Options: Options{ARSGap: &Gap{Blue: 130000, Official: 100000}},
				},
				Expect: Expect{
					Prices:   map[string]string{"BTC": "0.00300000", "ARS": "390000.00"},
					Sources:  map[string][]string{"ARS": {"criptoya:bitso/btc", "official/btc"}},
					Rejected: map[string][]string{"ARS": nil},
				},
			},
			{
				Name:   "xmr/btc alone",
				Input:  Input{Rates: []Rate{mkRate("kraken", "XMR/BTC", "0.00305000", 0)}},
				Expect: Expect{Prices: map[string]string{"BTC": "0.00305000"}},
			},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				opt := test.Input.Options
				opt.Now = func() time.Time { return aggNow }
				out := Aggregate(test.Input.Rates, opt)

				got := map[string]string{}
				for q, p := range out {
					got[q] = p.String()
					if p.Quote != q {
						t.Errorf("%s: Quote field is %q", q, p.Quote)
					}
				}
				if !maps.Equal(got, test.Expect.Prices) {
					t.Fatalf("got  %v\nwant %v", got, test.Expect.Prices)
				}
				for q, want := range test.Expect.Sources {
					if !slices.Equal(out[q].Sources, want) {
						t.Errorf("%s sources %v, want %v", q, out[q].Sources, want)
					}
				}
				for q, want := range test.Expect.Rejected {
					if !slices.Equal(out[q].Rejected, want) {
						t.Errorf("%s rejected %v, want %v", q, out[q].Rejected, want)
					}
				}
				for q, want := range test.Expect.Age {
					if age := aggNow.Sub(out[q].Time); age != want {
						t.Errorf("%s is %v old, want %v", q, age, want)
					}
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input struct {
				Rates   []Rate
				Options Options
			}
			Expect struct {
				Quotes []string
			}
			Test struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "no rates", Input: Input{}, Expect: Expect{}},
			{
				Name: "every rate is stale",
				Input: Input{Rates: []Rate{
					mkRate("a", "XMR/USD", "300.00", 11*time.Minute),
					mkRate("kraken", "XMR/BTC", "0.00300000", time.Hour),
				}},
			},
			{
				Name: "btc markets without xmr/btc",
				Input: Input{Rates: []Rate{
					mkRate("bitstamp", "BTC/USD", "100000.00", 0),
					mkRate("bitstamp", "BTC/EUR", "90000.00", 0),
				}},
			},
			{
				Name: "xmr/btc only from an excluded source",
				Input: Input{Rates: []Rate{
					mkRate("binance", "XMR/BTC", "0.00300000", 0),
					mkRate("bitstamp", "BTC/USD", "100000.00", 0),
				}},
			},
			{Name: "excluded quote", Input: Input{Rates: []Rate{mkRate("x", "XMR/LBP", "26850000.00", 0)}}},
			{Name: "excluded for one source", Input: Input{Rates: []Rate{mkRate("kraken", "XMR/AED", "1100.00", 0)}}},
			{
				Name: "votes too far apart",
				Input: Input{
					Rates:   []Rate{mkRate("a", "XMR/USD", "100.00", 0), mkRate("b", "XMR/USD", "200.00", 0)},
					Options: Options{MaxDeviationBps: 1},
				},
			},
			{
				Name: "sum overflows",
				Input: Input{Rates: []Rate{
					rawRate("a", "XMR/USD", math.MaxUint64),
					rawRate("b", "XMR/USD", math.MaxUint64),
				}},
			},
			{
				Name: "derived price overflows",
				Input: Input{Rates: []Rate{
					rawRate("kraken", "XMR/BTC", 1<<63),
					rawRate("bitstamp", "BTC/USD", 1<<63),
				}},
				Expect: Expect{Quotes: []string{"BTC"}},
			},
			{
				Name: "ars gap overflows",
				Input: Input{
					Rates: []Rate{
						mkRate("kraken", "XMR/BTC", "0.00300000", 0),
						mkRate("official", "BTC/ARS", "100000000.00", 0),
					},
					Options: Options{ARSGap: &Gap{Blue: math.MaxUint64, Official: 1}},
				},
				Expect: Expect{Quotes: []string{"BTC"}},
			},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				opt := test.Input.Options
				opt.Now = func() time.Time { return aggNow }
				got := slices.Sorted(maps.Keys(Aggregate(test.Input.Rates, opt)))
				if !slices.Equal(got, test.Expect.Quotes) {
					t.Fatalf("got quotes %v, want %v", got, test.Expect.Quotes)
				}
			})
		}
	})
}

func TestWithin(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input struct {
				V, Ref, Bps uint64
			}
			Expect struct{}
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "equal", Input: Input{100, 100, 0}},
			{Name: "upper edge", Input: Input{105, 100, 500}},
			{Name: "lower edge", Input: Input{95, 100, 500}},
			{Name: "both zero", Input: Input{0, 0, 500}},
			{Name: "huge values", Input: Input{math.MaxUint64, math.MaxUint64 - 1, 1}},
			{Name: "full band near the top", Input: Input{math.MaxUint64, math.MaxUint64/2 + 1, 10000}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				in := test.Input
				if !within(in.V, in.Ref, in.Bps) {
					t.Fatalf("within(%d, %d, %d) = false", in.V, in.Ref, in.Bps)
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input struct {
				V, Ref, Bps uint64
			}
			Expect struct{}
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "just above", Input: Input{106, 100, 500}},
			{Name: "just below", Input: Input{94, 100, 500}},
			{Name: "zero reference", Input: Input{1, 0, 10000}},
			{Name: "huge difference", Input: Input{math.MaxUint64, 1, 10000}},
			{Name: "zero band", Input: Input{101, 100, 0}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				in := test.Input
				if within(in.V, in.Ref, in.Bps) {
					t.Fatalf("within(%d, %d, %d) = true", in.V, in.Ref, in.Bps)
				}
			})
		}
	})
}

func TestPriceJSON(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input  struct{ Price Price }
			Expect struct{ String, JSON string }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{
				Name:  "fiat",
				Input: Input{Price{Quote: "USD", Value: 30123, Time: aggNow, Sources: []string{"a"}}},
				Expect: Expect{
					String: "301.23",
					JSON:   `{"quote":"USD","price":"301.23","time":"2026-11-07T12:00:00Z","sources":["a"]}`,
				},
			},
			{
				Name:  "btc with rejections",
				Input: Input{Price{Quote: "BTC", Value: 305000, Time: aggNow, Sources: []string{"a", "b"}, Rejected: []string{"evil"}}},
				Expect: Expect{
					String: "0.00305000",
					JSON:   `{"quote":"BTC","price":"0.00305000","time":"2026-11-07T12:00:00Z","sources":["a","b"],"rejected":["evil"]}`,
				},
			},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				if s := test.Input.Price.String(); s != test.Expect.String {
					t.Errorf("String() = %q", s)
				}
				b, err := json.Marshal(test.Input.Price)
				if err != nil || string(b) != test.Expect.JSON {
					t.Fatalf("got %s, %v", b, err)
				}
			})
		}
	})
}

// TestPipeline runs real clients against canned exchange replies, through
// FetchAll and Aggregate, the way the price worker does.
func TestPipeline(t *testing.T) {
	now := time.Now()
	sec, ms := now.Unix(), now.UnixMilli()
	routes := map[string]reply{
		"/0/public/Ticker": ok(`{"error":[],"result":{
			"XXMRZUSD":{"a":["301.00","1","1"],"b":["299.00","1","1"],"c":["300.00","1"]},
			"XXMRXXBT":{"a":["0.00310","1","1"],"b":["0.00300","1","1"],"c":["0.00305","1"]},
			"XMREUR":{"a":["281.0","1","1"],"b":["279.0","1","1"],"c":["280.0","1"]}}}`),
		"/markets/price": ok(fmt.Sprintf(`[{"symbol":"XMR_BTC","price":"0.00305","ts":%d}]`, ms)),
		"/api/v1/market/allTickers": ok(fmt.Sprintf(`{"code":"200000","data":{"time":%d,"ticker":[
			{"symbol":"XMR-BTC","buy":"0.0090","sell":"0.0092","last":"0.0091"}]}}`, ms)),
		"/api/v2/ticker/": ok(fmt.Sprintf(`[
			{"timestamp":"%[1]d","last":"100000","bid":"99990","ask":"100010","pair":"BTC/USD"},
			{"timestamp":"%[1]d","last":"90000","bid":"89990","ask":"90010","pair":"BTC/EUR"}]`, sec)),
		"/api/btc/ars/0.1": ok(fmt.Sprintf(`{"bitso":{"ask":130000000,"time":%d}}`, sec)),
		"/exrates":         ok(fmt.Sprintf(`{"BTC":100000,"USD":{"ARS":1300},"base":"USD","timestamp":%d}`, ms)),
		"/ticker":          ok(`{"BTC_TL":{"last":4000000,"lowestAsk":4001000,"highestBid":3999000}}`),
		"/v2/latest":       ok(`{"oficial":{"value_sell":1000},"blue":{"value_sell":1300}}`),
	}
	sources := func(u string) []Source {
		return []Source{
			Kraken{BaseURL: u}, Poloniex{BaseURL: u}, KuCoin{BaseURL: u}, Bitstamp{BaseURL: u},
			CriptoYa{BaseURL: u}, Yadio{BaseURL: u}, Paribu{BaseURL: u},
		}
	}
	withDown := func(down []string) map[string]reply {
		m := maps.Clone(routes)
		for _, path := range down {
			m[path] = reply{500, "down"}
		}
		return m
	}
	pipeline := func(t *testing.T, down []string) (map[string]Price, []string) {
		srv := serve(t, withDown(down))
		gap, err := BlueLytics{BaseURL: srv.URL}.Gap(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		results := FetchAll(t.Context(), 5*time.Second, sources(srv.URL)...)
		var failed []string
		for _, r := range results {
			if r.Err != nil {
				failed = append(failed, r.Source)
			}
		}
		slices.Sort(failed)
		return Aggregate(AllRates(results), Options{ARSGap: &gap}), failed
	}

	t.Run("Succeed", func(t *testing.T) {
		type (
			Input  struct{ Down []string }
			Expect struct {
				Prices   map[string]string
				Rejected map[string][]string
				Failed   []string
			}
			Test struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{
				Name:  "malicious source and a dead one",
				Input: Input{Down: []string{"/ticker"}},
				Expect: Expect{
					Prices:   map[string]string{"BTC": "0.00305000", "USD": "302.50", "EUR": "277.25", "ARS": "396500.00"},
					Rejected: map[string][]string{"BTC": {"kucoin"}, "USD": nil, "ARS": nil},
					Failed:   []string{"paribu"},
				},
			},
			{
				Name:  "fiat survives without direct xmr markets",
				Input: Input{Down: []string{"/ticker", "/0/public/Ticker", "/api/v1/market/allTickers"}},
				Expect: Expect{
					Prices: map[string]string{"BTC": "0.00305000", "USD": "305.00", "EUR": "274.50", "ARS": "396500.00"},
					Failed: []string{"kraken", "kucoin", "paribu"},
				},
			},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				out, failed := pipeline(t, test.Input.Down)
				got := map[string]string{}
				for q, p := range out {
					got[q] = p.String()
				}
				if !maps.Equal(got, test.Expect.Prices) {
					t.Fatalf("got  %v\nwant %v", got, test.Expect.Prices)
				}
				for q, want := range test.Expect.Rejected {
					if !slices.Equal(out[q].Rejected, want) {
						t.Errorf("%s rejected %v, want %v", q, out[q].Rejected, want)
					}
				}
				if !slices.Equal(failed, test.Expect.Failed) {
					t.Errorf("failed sources %v, want %v", failed, test.Expect.Failed)
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input  struct{ Down []string }
			Expect struct{ Failed []string }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		all := []string{"/0/public/Ticker", "/markets/price", "/api/v1/market/allTickers", "/api/v2/ticker/", "/api/btc/ars/0.1", "/exrates", "/ticker"}
		tests := []Test{
			{
				Name:   "every exchange down",
				Input:  Input{Down: all},
				Expect: Expect{Failed: []string{"bitstamp", "criptoya", "kraken", "kucoin", "paribu", "poloniex", "yadio"}},
			},
			{
				Name:   "only btc markets answer",
				Input:  Input{Down: []string{"/0/public/Ticker", "/markets/price", "/api/v1/market/allTickers", "/ticker"}},
				Expect: Expect{Failed: []string{"kraken", "kucoin", "paribu", "poloniex"}},
			},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				out, failed := pipeline(t, test.Input.Down)
				if len(out) != 0 {
					t.Fatalf("got prices %v", slices.Sorted(maps.Keys(out)))
				}
				if !slices.Equal(failed, test.Expect.Failed) {
					t.Errorf("failed sources %v, want %v", failed, test.Expect.Failed)
				}
			})
		}
	})
}
