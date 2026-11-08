package sources

import "testing"

func TestIsFiat(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input  struct{ Code string }
			Expect struct{ Fiat bool }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "dollar", Input: Input{"USD"}, Expect: Expect{true}},
			{Name: "euro", Input: Input{"EUR"}, Expect: Expect{true}},
			{Name: "lower case", Input: Input{"ars"}, Expect: Expect{true}},
			{Name: "lebanese pound", Input: Input{"LBP"}, Expect: Expect{true}},
			{Name: "new caribbean guilder", Input: Input{"XCG"}, Expect: Expect{true}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				if got := IsFiat(test.Input.Code); got != test.Expect.Fiat {
					t.Fatalf("IsFiat(%q) = %v", test.Input.Code, got)
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input  struct{ Code string }
			Expect struct{ Fiat bool }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "bitcoin", Input: Input{"BTC"}, Expect: Expect{false}},
			{Name: "monero", Input: Input{"XMR"}, Expect: Expect{false}},
			{Name: "tether", Input: Input{"USDT"}, Expect: Expect{false}},
			{Name: "gold", Input: Input{"XAU"}, Expect: Expect{false}},
			{Name: "special drawing rights", Input: Input{"XDR"}, Expect: Expect{false}},
			{Name: "lira as paribu writes it", Input: Input{"TL"}, Expect: Expect{false}},
			{Name: "empty", Input: Input{""}, Expect: Expect{false}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				if got := IsFiat(test.Input.Code); got != test.Expect.Fiat {
					t.Fatalf("IsFiat(%q) = %v", test.Input.Code, got)
				}
			})
		}
	})
}
