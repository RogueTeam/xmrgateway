package sources

import "testing"

func TestKrakenPair(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input  struct{ Key string }
			Expect struct{ Base, Quote string }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "legacy xmr fiat", Input: Input{"XXMRZUSD"}, Expect: Expect{"XMR", "USD"}},
			{Name: "legacy xmr btc", Input: Input{"XXMRXXBT"}, Expect: Expect{"XMR", "XBT"}},
			{Name: "legacy btc fiat", Input: Input{"XXBTZEUR"}, Expect: Expect{"XBT", "EUR"}},
			{Name: "new btc fiat", Input: Input{"XBTCHF"}, Expect: Expect{"XBT", "CHF"}},
			{Name: "new xmr fiat", Input: Input{"XMREUR"}, Expect: Expect{"XMR", "EUR"}},
			{Name: "four letter quote kept", Input: Input{"XMRUSDT"}, Expect: Expect{"XMR", "USDT"}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				base, quote, ok := krakenPair(test.Input.Key)
				if !ok || base != test.Expect.Base || quote != test.Expect.Quote {
					t.Fatalf("got %q %q %v", base, quote, ok)
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input  struct{ Key string }
			Expect struct{ OK bool }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "altcoin", Input: Input{"XETHZUSD"}, Expect: Expect{false}},
			{Name: "dark pool", Input: Input{"XBTUSD.d"}, Expect: Expect{false}},
			{Name: "slash", Input: Input{"XBT/USD"}, Expect: Expect{false}},
			{Name: "empty", Input: Input{""}, Expect: Expect{false}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				if base, quote, ok := krakenPair(test.Input.Key); ok != test.Expect.OK {
					t.Fatalf("parsed as %s/%s", base, quote)
				}
			})
		}
	})
}

func TestBitfinexPair(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input  struct{ Symbol string }
			Expect struct{ Base, Quote string }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "six letters", Input: Input{"XMRUSD"}, Expect: Expect{"XMR", "USD"}},
			{Name: "long codes", Input: Input{"TESTBTC:TESTUSD"}, Expect: Expect{"TESTBTC", "TESTUSD"}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				base, quote, ok := bitfinexPair(test.Input.Symbol)
				if !ok || base != test.Expect.Base || quote != test.Expect.Quote {
					t.Fatalf("got %q %q %v", base, quote, ok)
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input  struct{ Symbol string }
			Expect struct{ OK bool }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "too short", Input: Input{"XMRUS"}, Expect: Expect{false}},
			{Name: "too long without colon", Input: Input{"XMRUSDT"}, Expect: Expect{false}},
			{Name: "empty", Input: Input{""}, Expect: Expect{false}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				if base, quote, ok := bitfinexPair(test.Input.Symbol); ok != test.Expect.OK {
					t.Fatalf("parsed as %s/%s", base, quote)
				}
			})
		}
	})
}
