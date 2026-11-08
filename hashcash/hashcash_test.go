package hashcash

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"
)

var (
	t0  = time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	ist = time.FixedZone("IST", 5*3600+30*60)
	nst = time.FixedZone("NST", -(3*3600 + 30*60))
)

// zeroAt hashes to all zeroes only for the stamp carrying counter n, so the
// search must stop exactly there.
func zeroAt(n uint64) HashFunc {
	suffix := []byte(":" + base64.StdEncoding.EncodeToString(binary.BigEndian.AppendUint64(nil, n)))
	return func(in []byte) (out [28]byte) {
		if !bytes.HasSuffix(in, suffix) {
			out[0] = 0xff
		}
		return out
	}
}

func never(in []byte) (out [28]byte) {
	out[0] = 0xff
	return out
}

// only accepts one exact stamp.
func only(stamp string) HashFunc {
	return func(in []byte) (out [28]byte) {
		if string(in) != stamp {
			out[0] = 0xff
		}
		return out
	}
}

func mint(t *testing.T, d Details, h HashFunc) string {
	t.Helper()
	hc, err := d.Hashcash(t.Context(), h)
	if err != nil {
		t.Fatal(err)
	}
	return hc
}

func TestCountLeadingBits(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input  struct{ Bytes []byte }
			Expect struct{ Bits int }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "empty", Input: Input{nil}, Expect: Expect{0}},
			{Name: "top bit set", Input: Input{[]byte{0x80, 0}}, Expect: Expect{0}},
			{Name: "low bit of first byte", Input: Input{[]byte{0x01}}, Expect: Expect{7}},
			{Name: "crosses a byte", Input: Input{[]byte{0x00, 0x10, 0xff}}, Expect: Expect{11}},
			{Name: "stops at first set bit", Input: Input{[]byte{0x00, 0x00, 0x00, 0x01, 0x00}}, Expect: Expect{31}},
			{Name: "all zero", Input: Input{make([]byte, 28)}, Expect: Expect{224}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				if got := CountLeadingBits(test.Input.Bytes); got != test.Expect.Bits {
					t.Fatalf("got %d, want %d", got, test.Expect.Bits)
				}
				if len(test.Input.Bytes) == 28 {
					if got := CountLeadingBits([28]byte(test.Input.Bytes)); got != test.Expect.Bits {
						t.Fatalf("array form got %d", got)
					}
				}
			})
		}
	})
}

func TestHashcash(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input struct {
				Details Details
				Hash    HashFunc
			}
			Expect struct {
				Stamp   string
				Counter uint64
			}
			Test struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{
				Name:   "first counter wins at zero difficulty",
				Input:  Input{Details{Payload: "p", Extension: "e", Random: "r", Time: t0}, never},
				Expect: Expect{Stamp: "1:0:2026-11-07T12/00/00Z:p:e:r:AAAAAAAAAAA=", Counter: 0},
			},
			{
				Name:   "search stops at the first match",
				Input:  Input{Details{LeadingZeroes: 20, Payload: "p", Random: "r", Time: t0}, zeroAt(5)},
				Expect: Expect{Stamp: "1:20:2026-11-07T12/00/00Z:p::r:AAAAAAAAAAU=", Counter: 5},
			},
			{
				Name:   "more bits than asked still count",
				Input:  Input{Details{LeadingZeroes: 1, Time: t0}, zeroAt(300)},
				Expect: Expect{Stamp: "1:1:2026-11-07T12/00/00Z::::AAAAAAAAASw=", Counter: 300},
			},
			{
				Name:   "time keeps its zone and fraction",
				Input:  Input{Details{LeadingZeroes: 8, Payload: "p", Time: time.Date(2026, 11, 7, 17, 30, 0, 123456789, ist)}, zeroAt(0)},
				Expect: Expect{Stamp: "1:8:2026-11-07T17/30/00.123456789+0530:p:::AAAAAAAAAAA=", Counter: 0},
			},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				d := test.Input.Details
				hc := mint(t, d, test.Input.Hash)
				if hc != test.Expect.Stamp {
					t.Fatalf("got  %s\nwant %s", hc, test.Expect.Stamp)
				}
				d.Counter = 0
				if _, err := d.Hashcash(t.Context(), test.Input.Hash); err != nil || d.Counter != test.Expect.Counter {
					t.Fatalf("counter %d, %v", d.Counter, err)
				}
			})
		}
	})

	t.Run("Succeed with sha224", func(t *testing.T) {
		if testing.Short() {
			t.Skip("mints a few thousand real stamps")
		}
		type (
			Input  struct{ Bits, Runs int }
			Expect struct{ MaxAverage float64 }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		// Mean attempts should be 2^bits; requiring an exact match would double it.
		tests := []Test{
			{Name: "8 bits", Input: Input{8, 2000}, Expect: Expect{1.3 * 256}},
			{Name: "12 bits", Input: Input{12, 300}, Expect: Expect{1.3 * 4096}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				var total uint64
				for i := range test.Input.Runs {
					d := Details{LeadingZeroes: test.Input.Bits, Random: strconv.Itoa(i), Time: t0}
					hc := mint(t, d, sha256.Sum224)
					if err := Verify(sha256.Sum224, hc, test.Input.Bits); err != nil {
						t.Fatal(err)
					}
					d.Hashcash(t.Context(), sha256.Sum224)
					total += d.Counter + 1
				}
				if avg := float64(total) / float64(test.Input.Runs); avg > test.Expect.MaxAverage {
					t.Fatalf("average %.0f attempts, want at most %.0f", avg, test.Expect.MaxAverage)
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input struct {
				Context func(t *testing.T) context.Context
			}
			Expect struct {
				Err      error
				MaxTaken time.Duration
			}
			Test struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{
				Name: "already canceled",
				Input: Input{func(t *testing.T) context.Context {
					ctx, cancel := context.WithCancel(t.Context())
					cancel()
					return ctx
				}},
				Expect: Expect{context.Canceled, 50 * time.Millisecond},
			},
			{
				Name: "deadline while searching",
				Input: Input{func(t *testing.T) context.Context {
					ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
					t.Cleanup(cancel)
					return ctx
				}},
				Expect: Expect{context.DeadlineExceeded, 500 * time.Millisecond},
			},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				d := Details{LeadingZeroes: 1}
				start := time.Now()
				hc, err := d.Hashcash(test.Input.Context(t), never)
				if !errors.Is(err, test.Expect.Err) || hc != "" {
					t.Fatalf("got %q, %v", hc, err)
				}
				if took := time.Since(start); took > test.Expect.MaxTaken {
					t.Fatalf("stopping took %v", took)
				}
			})
		}
	})
}

func TestVerify(t *testing.T) {
	stamp12 := mint(t, Details{LeadingZeroes: 12, Payload: "p", Random: "r", Time: t0}, sha256.Sum224)

	t.Run("Succeed", func(t *testing.T) {
		type (
			Input struct {
				Stamp string
				Min   int
				Hash  HashFunc
			}
			Expect struct{}
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "exact difficulty", Input: Input{stamp12, 12, sha256.Sum224}},
			{Name: "stronger than required", Input: Input{stamp12, 8, sha256.Sum224}},
			{Name: "zero difficulty", Input: Input{mint(t, Details{Time: t0}, sha256.Sum224), 0, sha256.Sum224}},
			{Name: "fully zero hash meets any claim", Input: Input{"1:224:" + t0.Format(TimeLayout) + ":p::r:AAAAAAAAAAA=", 224, zeroAt(0)}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				if err := Verify(test.Input.Hash, test.Input.Stamp, test.Input.Min); err != nil {
					t.Fatal(err)
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input struct {
				Stamp string
				Min   int
				Hash  HashFunc
			}
			Expect struct{ ErrContains string }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "below the minimum", Input: Input{stamp12, 20, sha256.Sum224}, Expect: Expect{"at least a difficulty of: 20"}},
			{Name: "claims more bits than it has", Input: Input{strings.Replace(stamp12, "1:12:", "1:200:", 1), 12, sha256.Sum224}, Expect: Expect{"at least 200 bits"}},
			{Name: "tampered", Input: Input{strings.Replace(stamp12, ":r:", ":x:", 1), 12, only(stamp12)}, Expect: Expect{"but got 0"}},
			{Name: "too few fields", Input: Input{"1:12:x", 0, sha256.Sum224}, Expect: Expect{"invalid hashcash"}},
			{Name: "too many fields", Input: Input{stamp12 + ":x", 0, sha256.Sum224}, Expect: Expect{"invalid hashcash"}},
			{Name: "empty", Input: Input{"", 0, sha256.Sum224}, Expect: Expect{"invalid hashcash"}},
			{Name: "bits not a number", Input: Input{"1:x:t:p:e:r:c", 0, sha256.Sum224}, Expect: Expect{"bits part"}},
			{Name: "negative bits", Input: Input{"1:-1:t:p:e:r:c", 0, sha256.Sum224}, Expect: Expect{"bits part"}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				err := Verify(test.Input.Hash, test.Input.Stamp, test.Input.Min)
				if err == nil || !strings.Contains(err.Error(), test.Expect.ErrContains) {
					t.Fatalf("got %v, want %q", err, test.Expect.ErrContains)
				}
			})
		}
	})
}

func TestFromHashcash(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input  struct{ Stamp string }
			Expect struct{ Details Details }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{
				Name:   "literal stamp",
				Input:  Input{"1:20:2026-11-07T12/00/00Z:pay:ext:rnd:AAAAAAAAAAU="},
				Expect: Expect{Details{LeadingZeroes: 20, Time: t0, Payload: "pay", Extension: "ext", Random: "rnd", Counter: 5}},
			},
			{
				Name:   "empty fields",
				Input:  Input{"1:0:2026-11-07T12/00/00Z::::AAAAAAAAAAA="},
				Expect: Expect{Details{Time: t0}},
			},
			{
				Name:   "large counter",
				Input:  Input{"1:3:2026-11-07T12/00/00Z:p:e:r:f/////////8="},
				Expect: Expect{Details{LeadingZeroes: 3, Time: t0, Payload: "p", Extension: "e", Random: "r", Counter: 1<<63 - 1}},
			},
		}
		// Whatever Hashcash writes must come back unchanged.
		for _, d := range []Details{
			{LeadingZeroes: 4, Payload: "pay", Extension: "ext", Random: "rnd", Time: time.Date(2026, 11, 7, 17, 30, 0, 123456789, ist)},
			{LeadingZeroes: 4, Payload: "uuid-like-0f9e", Time: time.Date(2026, 11, 7, 8, 30, 0, 0, nst)},
			{LeadingZeroes: 4, Random: "r", Time: t0.Add(time.Microsecond)},
		} {
			d.Counter = 7
			tests = append(tests, Test{
				Name:   "round trip " + d.Time.Format(time.RFC3339Nano),
				Input:  Input{mint(t, d, zeroAt(7))},
				Expect: Expect{d},
			})
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				var got Details
				if err := got.FromHashcash(test.Input.Stamp); err != nil {
					t.Fatal(err)
				}
				want := test.Expect.Details
				if !got.Time.Equal(want.Time) {
					t.Errorf("time %v, want %v", got.Time, want.Time)
				}
				_, gotOff := got.Time.Zone()
				_, wantOff := want.Time.Zone()
				if gotOff != wantOff {
					t.Errorf("zone offset %d, want %d", gotOff, wantOff)
				}
				got.Time, want.Time = time.Time{}, time.Time{}
				if got != want {
					t.Fatalf("got  %+v\nwant %+v", got, want)
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input  struct{ Stamp string }
			Expect struct{ ErrContains string }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		ts := t0.Format(TimeLayout)
		tests := []Test{
			{Name: "incomplete", Input: Input{"1:20:" + ts + ":p:e:r"}, Expect: Expect{"incomplete"}},
			{Name: "wrong version", Input: Input{"2:20:" + ts + ":p:e:r:AAAAAAAAAAA="}, Expect: Expect{"version 1"}},
			{Name: "bits not a number", Input: Input{"1:x:" + ts + ":p:e:r:AAAAAAAAAAA="}, Expect: Expect{"leading zeroes"}},
			{Name: "bad time", Input: Input{"1:20:yesterday:p:e:r:AAAAAAAAAAA="}, Expect: Expect{"hashcash time"}},
			{Name: "time with colons is split", Input: Input{"1:20:2026-11-07T12:00:00Z:p:e:r:AAAAAAAAAAA="}, Expect: Expect{"incomplete"}},
			{Name: "counter not base64", Input: Input{"1:20:" + ts + ":p:e:r:!!!"}, Expect: Expect{"base64"}},
			{Name: "short counter", Input: Input{"1:20:" + ts + ":p:e:r:AA=="}, Expect: Expect{"8 bytes, got 1"}},
			{Name: "long counter", Input: Input{"1:20:" + ts + ":p:e:r:AAAAAAAAAAAA"}, Expect: Expect{"8 bytes, got 9"}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				var d Details
				err := d.FromHashcash(test.Input.Stamp)
				if err == nil || !strings.Contains(err.Error(), test.Expect.ErrContains) {
					t.Fatalf("got %v, want %q", err, test.Expect.ErrContains)
				}
				if d != (Details{}) {
					t.Fatalf("details filled on error: %+v", d)
				}
			})
		}
	})
}
