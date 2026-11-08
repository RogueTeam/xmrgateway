package ratelimit

import (
	"context"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)

type call struct {
	Key string
	At  time.Duration // after t0
}

type result struct {
	OK   bool
	Next time.Duration // after t0
}

func TestAllow(t *testing.T) {
	replay := func(interval time.Duration, calls []call) []result {
		l := New[string](interval)
		var got []result
		for _, c := range calls {
			next, ok := l.Allow(c.Key, t0.Add(c.At))
			got = append(got, result{ok, next.Sub(t0)})
		}
		return got
	}

	t.Run("Succeed", func(t *testing.T) {
		type (
			Input struct {
				Interval time.Duration
				Calls    []call
			}
			Expect struct{ Results []result }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{
				Name:   "first call",
				Input:  Input{30 * time.Second, []call{{"a", 0}}},
				Expect: Expect{[]result{{true, 30 * time.Second}}},
			},
			{
				Name:   "call exactly at next",
				Input:  Input{30 * time.Second, []call{{"a", 0}, {"a", 30 * time.Second}}},
				Expect: Expect{[]result{{true, 30 * time.Second}, {true, 60 * time.Second}}},
			},
			{
				Name:   "late call starts a new window",
				Input:  Input{30 * time.Second, []call{{"a", 0}, {"a", 95 * time.Second}}},
				Expect: Expect{[]result{{true, 30 * time.Second}, {true, 125 * time.Second}}},
			},
			{
				Name:   "keys are independent",
				Input:  Input{time.Minute, []call{{"a", 0}, {"b", time.Second}, {"c", 2 * time.Second}}},
				Expect: Expect{[]result{{true, time.Minute}, {true, 61 * time.Second}, {true, 62 * time.Second}}},
			},
			{
				Name:   "zero interval never blocks",
				Input:  Input{0, []call{{"a", 0}, {"a", 0}, {"a", 0}}},
				Expect: Expect{[]result{{true, 0}, {true, 0}, {true, 0}}},
			},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				if got := replay(test.Input.Interval, test.Input.Calls); !slices.Equal(got, test.Expect.Results) {
					t.Fatalf("got  %v\nwant %v", got, test.Expect.Results)
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input struct {
				Interval time.Duration
				Calls    []call
			}
			Expect struct{ Results []result }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{
				Name:   "second call in the window",
				Input:  Input{30 * time.Second, []call{{"a", 0}, {"a", 10 * time.Second}}},
				Expect: Expect{[]result{{true, 30 * time.Second}, {false, 30 * time.Second}}},
			},
			{
				Name:   "refusals do not extend the wait",
				Input:  Input{30 * time.Second, []call{{"a", 0}, {"a", 10 * time.Second}, {"a", 29 * time.Second}, {"a", 30 * time.Second}}},
				Expect: Expect{[]result{{true, 30 * time.Second}, {false, 30 * time.Second}, {false, 30 * time.Second}, {true, 60 * time.Second}}},
			},
			{
				Name:   "one nanosecond early",
				Input:  Input{30 * time.Second, []call{{"a", 0}, {"a", 30*time.Second - 1}}},
				Expect: Expect{[]result{{true, 30 * time.Second}, {false, 30 * time.Second}}},
			},
			{
				Name:   "clock going backwards",
				Input:  Input{30 * time.Second, []call{{"a", time.Minute}, {"a", 0}}},
				Expect: Expect{[]result{{true, 90 * time.Second}, {false, 90 * time.Second}}},
			},
			{
				Name:   "only the busy key waits",
				Input:  Input{time.Minute, []call{{"a", 0}, {"b", time.Second}, {"a", 2 * time.Second}, {"b", 3 * time.Second}}},
				Expect: Expect{[]result{{true, time.Minute}, {true, 61 * time.Second}, {false, time.Minute}, {false, 61 * time.Second}}},
			},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				if got := replay(test.Input.Interval, test.Input.Calls); !slices.Equal(got, test.Expect.Results) {
					t.Fatalf("got  %v\nwant %v", got, test.Expect.Results)
				}
			})
		}
	})
}

func TestSweep(t *testing.T) {
	// sweep registers calls, sweeps at sweepAt and reports what is left
	// and whether a call to key at at is then allowed.
	sweep := func(calls []call, sweepAt time.Duration, after call) (removed, left int, ok bool) {
		l := New[string](time.Minute)
		for _, c := range calls {
			l.Allow(c.Key, t0.Add(c.At))
		}
		removed = l.Sweep(t0.Add(sweepAt))
		left = l.Len()
		_, ok = l.Allow(after.Key, t0.Add(after.At))
		return removed, left, ok
	}

	t.Run("Succeed", func(t *testing.T) {
		type (
			Input struct {
				Calls   []call
				SweepAt time.Duration
				After   call
			}
			Expect struct{ Removed, Left int }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{
				Name:   "expired keys are forgotten",
				Input:  Input{[]call{{"old", 0}, {"recent", 50 * time.Second}}, time.Minute, call{"old", 70 * time.Second}},
				Expect: Expect{Removed: 1, Left: 1},
			},
			{
				Name:   "everything expired",
				Input:  Input{[]call{{"a", 0}, {"b", 0}, {"c", 0}}, time.Hour, call{"a", time.Hour}},
				Expect: Expect{Removed: 3, Left: 0},
			},
			{
				Name:   "empty limiter",
				Input:  Input{nil, 0, call{"a", 0}},
				Expect: Expect{Removed: 0, Left: 0},
			},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				removed, left, ok := sweep(test.Input.Calls, test.Input.SweepAt, test.Input.After)
				if removed != test.Expect.Removed || left != test.Expect.Left || !ok {
					t.Fatalf("removed %d, left %d, allowed after %v", removed, left, ok)
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input struct {
				Calls   []call
				SweepAt time.Duration
				After   call
			}
			Expect struct{ Removed, Left int }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{
				Name:   "waiting key survives",
				Input:  Input{[]call{{"old", 0}, {"recent", 50 * time.Second}}, time.Minute, call{"recent", 70 * time.Second}},
				Expect: Expect{Removed: 1, Left: 1},
			},
			{
				Name:   "sweep one nanosecond early",
				Input:  Input{[]call{{"a", 0}}, time.Minute - 1, call{"a", time.Minute - 1}},
				Expect: Expect{Removed: 0, Left: 1},
			},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				removed, left, ok := sweep(test.Input.Calls, test.Input.SweepAt, test.Input.After)
				if removed != test.Expect.Removed || left != test.Expect.Left || ok {
					t.Fatalf("removed %d, left %d, allowed after %v", removed, left, ok)
				}
			})
		}
	})
}

func TestConcurrent(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input struct {
				Goroutines int
				Keys       []string
			}
			Expect struct{ Wins int64 }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "one key", Input: Input{100, []string{"k"}}, Expect: Expect{1}},
			{Name: "many keys", Input: Input{200, []string{"a", "b", "c", "d"}}, Expect: Expect{4}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				l := New[string](time.Minute)
				var wins atomic.Int64
				var wg sync.WaitGroup
				for i := range test.Input.Goroutines {
					wg.Go(func() {
						if _, ok := l.Allow(test.Input.Keys[i%len(test.Input.Keys)], t0); ok {
							wins.Add(1)
						}
						l.Sweep(t0)
					})
				}
				wg.Wait()
				if wins.Load() != test.Expect.Wins {
					t.Fatalf("%d calls allowed, want %d", wins.Load(), test.Expect.Wins)
				}
			})
		}
	})
}

func TestRun(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input  struct{ Every time.Duration }
			Expect struct{ Within time.Duration }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "sweeps expired keys and stops", Input: Input{5 * time.Millisecond}, Expect: Expect{2 * time.Second}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				l := New[string](time.Millisecond)
				l.Allow("a", time.Now())
				ctx, cancel := context.WithCancel(t.Context())
				done := make(chan struct{})
				go func() {
					l.Run(ctx, test.Input.Every)
					close(done)
				}()
				deadline := time.Now().Add(test.Expect.Within)
				for l.Len() != 0 {
					if time.Now().After(deadline) {
						t.Fatal("key never swept")
					}
					time.Sleep(time.Millisecond)
				}
				cancel()
				select {
				case <-done:
				case <-time.After(test.Expect.Within):
					t.Fatal("Run did not return after cancel")
				}
			})
		}
	})
}
