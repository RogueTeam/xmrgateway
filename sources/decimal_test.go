package sources

import (
	"errors"
	"math"
	"testing"
)

func TestParseDecimal(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input struct {
				Text  string
				Scale int
			}
			Expect struct {
				Value uint64
			}
			Test struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "two decimals", Input: Input{"312.45", 2}, Expect: Expect{31245}},
			{Name: "rounds half up", Input: Input{"312.455", 2}, Expect: Expect{31246}},
			{Name: "rounds down", Input: Input{"312.4549", 2}, Expect: Expect{31245}},
			{Name: "integer", Input: Input{"312", 2}, Expect: Expect{31200}},
			{Name: "leading zeros", Input: Input{"000123.10", 2}, Expect: Expect{12310}},
			{Name: "no integer part", Input: Input{".5", 2}, Expect: Expect{50}},
			{Name: "trailing dot", Input: Input{"7.", 2}, Expect: Expect{700}},
			{Name: "satoshis", Input: Input{"0.00305", 8}, Expect: Expect{305000}},
			{Name: "negative exponent", Input: Input{"3.05e-3", 8}, Expect: Expect{305000}},
			{Name: "positive exponent", Input: Input{"3.05E+4", 2}, Expect: Expect{3050000}},
			{Name: "bare exponent", Input: Input{"1e5", 2}, Expect: Expect{10000000}},
			{Name: "below half a cent", Input: Input{"0.004", 2}, Expect: Expect{0}},
			{Name: "half a cent", Input: Input{"0.005", 2}, Expect: Expect{1}},
			{Name: "rounds up into scale", Input: Input{"9e-9", 8}, Expect: Expect{1}},
			{Name: "zero", Input: Input{"0", 2}, Expect: Expect{0}},
			{Name: "surrounding spaces", Input: Input{" 1.5 ", 2}, Expect: Expect{150}},
			{Name: "scale zero", Input: Input{"42.6", 0}, Expect: Expect{43}},
			{Name: "largest value", Input: Input{"18446744073709551615", 0}, Expect: Expect{math.MaxUint64}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				got, err := ParseDecimal(test.Input.Text, test.Input.Scale)
				if err != nil {
					t.Fatal(err)
				}
				if got != test.Expect.Value {
					t.Fatalf("got %d, want %d", got, test.Expect.Value)
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input struct {
				Text  string
				Scale int
			}
			Expect struct {
				Err error
			}
			Test struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "empty", Input: Input{"", 2}, Expect: Expect{errDecimal}},
			{Name: "only a dot", Input: Input{".", 2}, Expect: Expect{errDecimal}},
			{Name: "negative", Input: Input{"-1", 2}, Expect: Expect{errDecimal}},
			{Name: "plus sign", Input: Input{"+1", 2}, Expect: Expect{errDecimal}},
			{Name: "two dots", Input: Input{"1.2.3", 2}, Expect: Expect{errDecimal}},
			{Name: "letters", Input: Input{"abc", 2}, Expect: Expect{errDecimal}},
			{Name: "NaN", Input: Input{"NaN", 2}, Expect: Expect{errDecimal}},
			{Name: "empty exponent", Input: Input{"1e", 2}, Expect: Expect{errDecimal}},
			{Name: "huge exponent", Input: Input{"1e999", 2}, Expect: Expect{errDecimal}},
			{Name: "overflow", Input: Input{"184467440737095516160", 2}, Expect: Expect{errDecimal}},
			{Name: "overflow from rounding", Input: Input{"18446744073709551615.9", 0}, Expect: Expect{errDecimal}},
			{Name: "comma decimal", Input: Input{"312,45", 2}, Expect: Expect{errDecimal}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				if _, err := ParseDecimal(test.Input.Text, test.Input.Scale); !errors.Is(err, test.Expect.Err) {
					t.Fatalf("got %v, want %v", err, test.Expect.Err)
				}
			})
		}
	})
}

func TestFormatDecimal(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input struct {
				Value uint64
				Scale int
			}
			Expect struct {
				Text string
			}
			Test struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "cents", Input: Input{31245, 2}, Expect: Expect{"312.45"}},
			{Name: "below one", Input: Input{5, 2}, Expect: Expect{"0.05"}},
			{Name: "zero", Input: Input{0, 2}, Expect: Expect{"0.00"}},
			{Name: "satoshis", Input: Input{305000, 8}, Expect: Expect{"0.00305000"}},
			{Name: "whole bitcoin", Input: Input{100000000, 8}, Expect: Expect{"1.00000000"}},
			{Name: "no decimals", Input: Input{7, 0}, Expect: Expect{"7"}},
			{Name: "largest value", Input: Input{math.MaxUint64, 2}, Expect: Expect{"184467440737095516.15"}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				if got := FormatDecimal(test.Input.Value, test.Input.Scale); got != test.Expect.Text {
					t.Fatalf("got %q, want %q", got, test.Expect.Text)
				}
				back, err := ParseDecimal(test.Expect.Text, test.Input.Scale)
				if err != nil || back != test.Input.Value {
					t.Fatalf("round trip gave %d, %v", back, err)
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input struct {
				Value uint64
				Scale int
			}
			Expect struct {
				NotText string
			}
			Test struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "never drops the leading zero", Input: Input{5, 2}, Expect: Expect{".05"}},
			{Name: "never drops trailing zeros", Input: Input{300, 2}, Expect: Expect{"3"}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				if got := FormatDecimal(test.Input.Value, test.Input.Scale); got == test.Expect.NotText {
					t.Fatalf("got %q", got)
				}
			})
		}
	})
}

func TestMulDiv(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input  struct{ A, B, C uint64 }
			Expect struct{ Value uint64 }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "xmr via btc", Input: Input{305000, 9000000, 1e8}, Expect: Expect{27450}},
			{Name: "rounds half up", Input: Input{1, 1, 2}, Expect: Expect{1}},
			{Name: "rounds down", Input: Input{1, 1, 3}, Expect: Expect{0}},
			{Name: "needs 128 bits", Input: Input{math.MaxUint64, math.MaxUint64, math.MaxUint64}, Expect: Expect{math.MaxUint64}},
			{Name: "zero factor", Input: Input{0, 12345, 7}, Expect: Expect{0}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				got, ok := mulDiv(test.Input.A, test.Input.B, test.Input.C)
				if !ok || got != test.Expect.Value {
					t.Fatalf("got %d, %v; want %d", got, ok, test.Expect.Value)
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input  struct{ A, B, C uint64 }
			Expect struct{ OK bool }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "zero divisor", Input: Input{1, 1, 0}, Expect: Expect{false}},
			{Name: "result overflows", Input: Input{math.MaxUint64, 2, 1}, Expect: Expect{false}},
			{Name: "rounding overflows", Input: Input{31, 1190112520884487201, 2}, Expect: Expect{false}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				if _, ok := mulDiv(test.Input.A, test.Input.B, test.Input.C); ok != test.Expect.OK {
					t.Fatalf("ok = %v", ok)
				}
			})
		}
	})
}

func TestScale(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input  struct{ Quote string }
			Expect struct{ Scale int }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "bitcoin", Input: Input{"BTC"}, Expect: Expect{BTCScale}},
			{Name: "dollar", Input: Input{"USD"}, Expect: Expect{FiatScale}},
			{Name: "yen", Input: Input{"JPY"}, Expect: Expect{FiatScale}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				if got := Scale(test.Input.Quote); got != test.Expect.Scale {
					t.Fatalf("got %d", got)
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input  struct{ Quote string }
			Expect struct{ NotScale int }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "lower case btc is not bitcoin", Input: Input{"btc"}, Expect: Expect{BTCScale}},
			{Name: "xbt is not normalized here", Input: Input{"XBT"}, Expect: Expect{BTCScale}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				if got := Scale(test.Input.Quote); got == test.Expect.NotScale {
					t.Fatalf("got %d", got)
				}
			})
		}
	})
}
