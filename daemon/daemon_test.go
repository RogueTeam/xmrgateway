package daemon

import (
	"bufio"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// superviseEnv makes the test binary supervise a sleeping child that writes
// its pid to the given directory, so a test can kill the supervisor.
const superviseEnv = "DAEMON_TEST_SUPERVISE"

func TestMain(m *testing.M) {
	if dir := os.Getenv(superviseEnv); dir != "" {
		New(context.Background(), &Config{
			ControlContext: context.Background(),
			Executable:     "/bin/sh",
			Arguments:      []string{"-c", sleeper, dir},
		})
		for {
			time.Sleep(time.Hour)
		}
	}
	os.Exit(m.Run())
}

// Scripts get their directory as $0.
const (
	sleeper  = `echo $$ > "$0/pid"; exec sleep 60`
	crasher  = `echo run >> "$0/runs"; exit 1`
	stubborn = `trap "" INT; echo $$ > "$0/pid"; while :; do sleep 1; done`
)

func eventually(within time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(within)
	for !cond() {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(20 * time.Millisecond)
	}
	return true
}

func alive(pid int) bool { return syscall.Kill(pid, 0) == nil }

func runs(dir string) int {
	b, _ := os.ReadFile(filepath.Join(dir, "runs"))
	return strings.Count(string(b), "run\n")
}

// pid waits for the child to write its pid.
func pid(t *testing.T, dir string) int {
	t.Helper()
	var p int
	if !eventually(5*time.Second, func() bool {
		b, _ := os.ReadFile(filepath.Join(dir, "pid"))
		p, _ = strconv.Atoi(strings.TrimSpace(string(b)))
		return p > 0
	}) {
		t.Fatal("child never started")
	}
	return p
}

func finished(d *Daemon, within time.Duration) bool {
	select {
	case <-d.notifyKill:
		return true
	case <-time.After(within):
		return false
	}
}

func start(t *testing.T, ctx context.Context, script string, interval time.Duration, expires time.Time) (*Daemon, string) {
	dir := t.TempDir()
	d := New(t.Context(), &Config{
		ControlContext: ctx,
		Interval:       interval,
		ExpiresAt:      expires,
		Executable:     "/bin/sh",
		Arguments:      []string{"-c", script, dir},
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		d.Kill(ctx)
	})
	return d, dir
}

func TestRestart(t *testing.T) {
	t.Parallel()
	t.Run("Succeed", func(t *testing.T) {
		t.Parallel()
		type (
			Input struct {
				Interval time.Duration
				Script   string
			}
			Expect struct{ MinGap, MaxGap time.Duration }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "crash waits for the interval", Input: Input{2 * time.Second, crasher}, Expect: Expect{1700 * time.Millisecond, 3 * time.Second}},
			{Name: "interval below the minimum is raised", Input: Input{100 * time.Millisecond, crasher}, Expect: Expect{1700 * time.Millisecond, 3 * time.Second}},
			{
				Name:   "long run is restarted right away",
				Input:  Input{2 * time.Second, `echo run >> "$0/runs"; sleep 3; exit 1`},
				Expect: Expect{2900 * time.Millisecond, 3800 * time.Millisecond},
			},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				t.Parallel()
				began := time.Now()
				_, dir := start(t, t.Context(), test.Input.Script, test.Input.Interval, time.Time{})
				if !eventually(time.Second, func() bool { return runs(dir) == 1 }) {
					t.Fatal("first start was not immediate")
				}
				first := time.Now()
				if !eventually(test.Expect.MaxGap+time.Second, func() bool { return runs(dir) == 2 }) {
					t.Fatal("never restarted")
				}
				if gap := time.Since(first); gap < test.Expect.MinGap || gap > test.Expect.MaxGap {
					t.Fatalf("restarted %v after the first start (began %v before), want %v to %v", gap, first.Sub(began), test.Expect.MinGap, test.Expect.MaxGap)
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		t.Parallel()
		type (
			Input  struct{ Expires time.Duration }
			Expect struct{ Runs int }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "expired before the first start", Input: Input{-time.Second}, Expect: Expect{0}},
			{Name: "expired before the restart", Input: Input{time.Second}, Expect: Expect{1}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				t.Parallel()
				d, dir := start(t, t.Context(), crasher, 2*time.Second, time.Now().Add(test.Input.Expires))
				if !finished(d, 4*time.Second) {
					t.Fatal("daemon kept going after expiring")
				}
				if n := runs(dir); n != test.Expect.Runs {
					t.Fatalf("%d runs, want %d", n, test.Expect.Runs)
				}
			})
		}
	})
}

func TestStop(t *testing.T) {
	t.Parallel()
	t.Run("Succeed", func(t *testing.T) {
		t.Parallel()
		type (
			Input struct {
				Script  string
				Expires time.Duration
				Stop    func(cancel context.CancelFunc, d *Daemon)
			}
			Expect struct{ Within time.Duration }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		kill := func(_ context.CancelFunc, d *Daemon) { d.Kill(context.Background()) }
		cancel := func(cancel context.CancelFunc, _ *Daemon) { cancel() }
		wait := func(context.CancelFunc, *Daemon) {}
		tests := []Test{
			{Name: "Kill stops the child", Input: Input{Script: sleeper, Stop: kill}, Expect: Expect{3 * time.Second}},
			{Name: "canceling the control context stops the child", Input: Input{Script: sleeper, Stop: cancel}, Expect: Expect{3 * time.Second}},
			{Name: "expiry stops a running child", Input: Input{Script: sleeper, Expires: time.Second, Stop: wait}, Expect: Expect{4 * time.Second}},
			{Name: "stubborn child is killed after 5 seconds", Input: Input{Script: stubborn, Stop: kill}, Expect: Expect{8 * time.Second}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				t.Parallel()
				ctx, stop := context.WithCancel(t.Context())
				defer stop()
				var expires time.Time
				if test.Input.Expires != 0 {
					expires = time.Now().Add(test.Input.Expires)
				}
				d, dir := start(t, ctx, test.Input.Script, 2*time.Second, expires)
				child := pid(t, dir)

				go test.Input.Stop(stop, d)
				if !finished(d, test.Expect.Within) {
					t.Fatal("daemon did not finish")
				}
				if alive(child) {
					t.Fatalf("child %d still running", child)
				}
				if again := pid(t, dir); again != child {
					t.Fatalf("child restarted as %d", again)
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		t.Parallel()
		type (
			Input  struct{ KillTimeout time.Duration }
			Expect struct{ ReturnsWithin time.Duration }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "Kill gives up when its context ends", Input: Input{300 * time.Millisecond}, Expect: Expect{time.Second}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				t.Parallel()
				d, dir := start(t, t.Context(), stubborn, 2*time.Second, time.Time{})
				child := pid(t, dir)
				ctx, cancel := context.WithTimeout(context.Background(), test.Input.KillTimeout)
				defer cancel()
				began := time.Now()
				d.Kill(ctx)
				if took := time.Since(began); took > test.Expect.ReturnsWithin {
					t.Fatalf("Kill returned after %v", took)
				}
				if finished(d, 0) || !alive(child) {
					t.Fatal("child already gone; nothing was left to wait for")
				}
				if !finished(d, 8*time.Second) {
					t.Fatal("daemon never finished after Kill")
				}
			})
		}
	})
}

// TestParentDeath runs a supervisor in its own process and kills it, which is
// what the worker process is for.
func TestParentDeath(t *testing.T) {
	t.Parallel()
	supervisor := func(t *testing.T) (*exec.Cmd, int) {
		dir := t.TempDir()
		cmd := exec.Command(os.Args[0])
		cmd.Env = append(os.Environ(), superviseEnv+"="+dir)
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
		return cmd, pid(t, dir)
	}

	t.Run("Succeed", func(t *testing.T) {
		t.Parallel()
		type (
			Input  struct{ Signal syscall.Signal }
			Expect struct{ Within time.Duration }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "supervisor killed", Input: Input{syscall.SIGKILL}, Expect: Expect{4 * time.Second}},
			{Name: "supervisor terminated without cleanup", Input: Input{syscall.SIGTERM}, Expect: Expect{4 * time.Second}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				t.Parallel()
				cmd, child := supervisor(t)
				cmd.Process.Signal(test.Input.Signal)
				cmd.Wait()
				if !eventually(test.Expect.Within, func() bool { return !alive(child) }) {
					t.Fatalf("child %d outlived its supervisor", child)
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		t.Parallel()
		type (
			Input  struct{ Watch time.Duration }
			Expect struct{}
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "child keeps running while the supervisor lives", Input: Input{2500 * time.Millisecond}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				t.Parallel()
				_, child := supervisor(t)
				time.Sleep(test.Input.Watch)
				if !alive(child) {
					t.Fatal("child stopped while the supervisor was alive")
				}
			})
		}
	})
}

// TestWorker runs the hidden worker mode directly.
func TestWorker(t *testing.T) {
	t.Parallel()
	worker := func(t *testing.T, ppid string, rest ...string) *exec.Cmd {
		return exec.CommandContext(t.Context(), os.Args[0], append([]string{DaemonWorkerFlagname, ppid}, rest...)...)
	}
	self := strconv.Itoa(os.Getpid())
	exitCode := func(t *testing.T, err error) int {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return exit.ExitCode()
		}
		if err != nil {
			t.Fatal(err)
		}
		return 0
	}

	t.Run("Succeed", func(t *testing.T) {
		t.Parallel()
		type (
			Input  struct{ Script string }
			Expect struct {
				Code   int
				Stdout string
			}
			Test struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "clean exit", Input: Input{"exit 0"}, Expect: Expect{0, ""}},
			{Name: "exit code is passed through", Input: Input{"exit 7"}, Expect: Expect{7, ""}},
			{Name: "output is passed through", Input: Input{"echo hello"}, Expect: Expect{0, "hello\n"}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				out, err := worker(t, self, "/bin/sh", "-c", test.Input.Script).Output()
				if code := exitCode(t, err); code != test.Expect.Code || string(out) != test.Expect.Stdout {
					t.Fatalf("exit %d, stdout %q", code, out)
				}
			})
		}
	})

	t.Run("Succeed with signals", func(t *testing.T) {
		t.Parallel()
		type (
			Input  struct{ Signal syscall.Signal }
			Expect struct{ Code int }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "SIGTERM is forwarded", Input: Input{syscall.SIGTERM}, Expect: Expect{3}},
			{Name: "SIGINT is forwarded", Input: Input{syscall.SIGINT}, Expect: Expect{4}},
			{Name: "SIGHUP is forwarded", Input: Input{syscall.SIGHUP}, Expect: Expect{5}},
		}
		script := `trap "exit 3" TERM; trap "exit 4" INT; trap "exit 5" HUP; echo ready; while :; do sleep 0.1; done`
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				cmd := worker(t, self, "/bin/sh", "-c", script)
				stdout, _ := cmd.StdoutPipe()
				if err := cmd.Start(); err != nil {
					t.Fatal(err)
				}
				if line, _ := bufio.NewReader(stdout).ReadString('\n'); line != "ready\n" {
					t.Fatalf("child said %q", line)
				}
				cmd.Process.Signal(test.Input.Signal)
				if code := exitCode(t, cmd.Wait()); code != test.Expect.Code {
					t.Fatalf("exit %d, want %d", code, test.Expect.Code)
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		t.Parallel()
		type (
			Input struct {
				PPID string
				Rest []string
			}
			Expect struct {
				Code   int
				Stderr string
				Within time.Duration
			}
			Test struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "ppid is not a number", Input: Input{"me", []string{"/bin/sh"}}, Expect: Expect{1, "Expecting a valid integer as ppid", time.Second}},
			{Name: "no executable", Input: Input{self, nil}, Expect: Expect{1, "No executable defined for the main worker", time.Second}},
			{Name: "executable missing", Input: Input{self, []string{"/no/such/program"}}, Expect: Expect{1, "Failed to start child process", time.Second}},
			{Name: "parent already gone", Input: Input{"1", []string{"/bin/sh", "-c", "exec sleep 60"}}, Expect: Expect{255, "Parent died", 4 * time.Second}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				var stderr strings.Builder
				cmd := worker(t, test.Input.PPID, test.Input.Rest...)
				cmd.Stderr = &stderr
				began := time.Now()
				code := exitCode(t, cmd.Run())
				if code != test.Expect.Code || !strings.Contains(stderr.String(), test.Expect.Stderr) {
					t.Fatalf("exit %d, stderr %q", code, stderr.String())
				}
				if took := time.Since(began); took > test.Expect.Within {
					t.Fatalf("took %v", took)
				}
			})
		}
	})
}
