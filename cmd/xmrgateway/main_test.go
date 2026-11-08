package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/RogueTeam/xmrgateway/hashcash"
	"github.com/RogueTeam/xmrgateway/internal/config"
)

const argsEnv = "XMRGATEWAY_TEST_ARGS"

// TestMain lets the tests run the test binary as the real command, so exit
// codes, usage output and signal handling are checked as users see them.
func TestMain(m *testing.M) {
	if raw, ok := os.LookupEnv(argsEnv); ok {
		var args []string
		if err := json.Unmarshal([]byte(raw), &args); err != nil {
			panic(err)
		}
		os.Args = append([]string{"xmrgateway"}, args...)
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func process(t *testing.T, dir string, env []string, args ...string) *exec.Cmd {
	t.Helper()
	raw, _ := json.Marshal(args)
	cmd := exec.CommandContext(t.Context(), os.Args[0])
	cmd.Dir = dir
	cmd.Env = append(append(os.Environ(), argsEnv+"="+string(raw)), env...)
	return cmd
}

type result struct {
	Stdout, Stderr string
	Code           int
	Took           time.Duration
}

func cli(t *testing.T, dir string, env []string, args ...string) result {
	t.Helper()
	cmd := process(t, dir, env, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	start := time.Now()
	err := cmd.Run()
	r := result{stdout.String(), stderr.String(), 0, time.Since(start)}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		r.Code = exit.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return r
}

// validConfig listens on a free port and keeps upstream requests local.
func validConfig(mod func(*config.Config)) string {
	var cfg config.Config
	cfg.Default()
	cfg.Secret = strings.Repeat("s", 32)
	cfg.Listen = "127.0.0.1:0"
	cfg.UpstreamProxy = "http://127.0.0.1:1"
	cfg.RequestTimeout = config.Duration{Duration: 2 * time.Second}
	if mod != nil {
		mod(&cfg)
	}
	b, _ := json.MarshalIndent(cfg, "", "  ")
	return string(b)
}

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func expand(dir string, list []string) []string {
	out := make([]string, len(list))
	for i, s := range list {
		out[i] = strings.ReplaceAll(s, "{dir}", dir)
	}
	return out
}

func TestCLI(t *testing.T) {
	// invoke writes files into a fresh directory and runs the command there;
	// {dir} in args and env becomes that directory.
	invoke := func(t *testing.T, files map[string]string, env, args []string) (string, result) {
		dir := t.TempDir()
		writeFiles(t, dir, files)
		return dir, cli(t, dir, expand(dir, env), expand(dir, args)...)
	}
	stamp := func(payload string, bits int) func(t *testing.T, _ string, r result) {
		return func(t *testing.T, _ string, r result) {
			hc := strings.TrimSpace(r.Stdout)
			var d hashcash.Details
			if err := d.FromHashcash(hc); err != nil || d.Payload != payload {
				t.Fatalf("stamp %q: %v", hc, err)
			}
			if err := hashcash.Verify(sha256.Sum224, hc, bits); err != nil {
				t.Fatal(err)
			}
		}
	}
	exists := func(paths ...string) func(t *testing.T, dir string, _ result) {
		return func(t *testing.T, dir string, _ result) {
			for _, p := range paths {
				if _, err := os.Stat(filepath.Join(dir, p)); err != nil {
					t.Error(err)
				}
			}
		}
	}

	t.Run("Succeed", func(t *testing.T) {
		type (
			Input struct {
				Args  []string
				Files map[string]string
				Env   []string
			}
			Expect struct {
				Stdout   string
				Contains []string
				Stderr   []string
				Check    func(t *testing.T, dir string, r result)
			}
			Test struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "version", Input: Input{Args: []string{"version"}}, Expect: Expect{Stdout: "dev\n"}},
			{Name: "schema", Input: Input{Args: []string{"schema"}}, Expect: Expect{Stdout: string(config.JSONSchema)}},
			{Name: "check", Input: Input{Args: []string{"check"}, Files: map[string]string{"config.json": validConfig(nil)}}, Expect: Expect{Stdout: "config.json is valid\n"}},
			{Name: "check another file", Input: Input{Args: []string{"check", "-config", "other.json"}, Files: map[string]string{"other.json": validConfig(nil)}}, Expect: Expect{Stdout: "other.json is valid\n"}},
			{
				Name:   "check from home",
				Input:  Input{Args: []string{"check", "-chdir-home"}, Files: map[string]string{"home/config.json": validConfig(nil)}, Env: []string{"HOME={dir}/home"}},
				Expect: Expect{Stdout: "config.json is valid\n"},
			},
			{
				Name:   "check the shipped example",
				Input:  Input{Args: []string{"check", "-config", mustAbs(t, "../../config.example.json")}},
				Expect: Expect{Contains: []string{"config.example.json is valid"}},
			},
			{
				Name:   "deploy",
				Input:  Input{Args: []string{"deploy", "-dir", "out"}},
				Expect: Expect{Stdout: "static assets written to out\n", Check: exists("out/pico.orange.min.css", "out/logo.svg")},
			},
			{
				Name:   "deploy into home",
				Input:  Input{Args: []string{"deploy", "-chdir-home"}, Files: map[string]string{"home/.keep": ""}, Env: []string{"HOME={dir}/home"}},
				Expect: Expect{Stdout: "static assets written to static\n", Check: exists("home/static/logo.svg")},
			},
			{
				Name:  "install dry run",
				Input: Input{Args: []string{"install", "-dry-run"}},
				Expect: Expect{Contains: []string{
					"1. copy ", " to /usr/local/bin/xmrgateway\n",
					"2. write login class /etc/login.conf.d/xmrgateway\n",
					"3. create user _xmrgateway with uid 5000, class xmrgateway and home /home/_xmrgateway\n",
					"4. write /home/_xmrgateway/config.json with a random secret, unless it exists\n",
					"5. deploy static assets to /home/_xmrgateway/static\n",
					"6. write /etc/rc.d/xmrgateway\n",
				}},
			},
			{
				Name:   "install dry run with flags",
				Input:  Input{Args: []string{"install", "-dry-run", "-bin", "/opt/xmr", "-user", "_p", "-uid", "6000", "-home", "/var/p"}},
				Expect: Expect{Contains: []string{" to /opt/xmr\n", "create user _p with uid 6000", "/var/p/config.json", "/var/p/static"}},
			},
			{Name: "pow", Input: Input{Args: []string{"pow", "0d5e4bd2-2c32-4f3e-9a7a-2f6a0e1c9b11", "8"}}, Expect: Expect{Check: stamp("0d5e4bd2-2c32-4f3e-9a7a-2f6a0e1c9b11", 8)}},
			{Name: "pow with -until in the past", Input: Input{Args: []string{"pow", "-until", "2020-01-01T00:00:00Z", "p", "12"}}, Expect: Expect{Check: stamp("p", 12)}},
			{
				Name:  "pow waits for -until",
				Input: Input{Args: []string{"pow", "-until", time.Now().Add(time.Second).UTC().Format(time.RFC3339), "p", "4"}},
				Expect: Expect{Check: func(t *testing.T, dir string, r result) {
					stamp("p", 4)(t, dir, r)
					if r.Took < time.Second {
						t.Fatalf("printed after %v", r.Took)
					}
				}},
			},
			{Name: "pow help", Input: Input{Args: []string{"pow", "-h"}}, Expect: Expect{Stderr: []string{"usage: xmrgateway pow [flags] PAYLOAD DIFFICULTY", "-until"}}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				dir, r := invoke(t, test.Input.Files, test.Input.Env, test.Input.Args)
				if r.Code != 0 {
					t.Fatalf("exit %d\nstdout: %s\nstderr: %s", r.Code, r.Stdout, r.Stderr)
				}
				if test.Expect.Stdout != "" && r.Stdout != test.Expect.Stdout {
					t.Errorf("stdout %q, want %q", r.Stdout, test.Expect.Stdout)
				}
				for _, want := range test.Expect.Contains {
					if !strings.Contains(r.Stdout, want) {
						t.Errorf("stdout is missing %q:\n%s", want, r.Stdout)
					}
				}
				for _, want := range test.Expect.Stderr {
					if !strings.Contains(r.Stderr, want) {
						t.Errorf("stderr is missing %q:\n%s", want, r.Stderr)
					}
				}
				if test.Expect.Check != nil {
					test.Expect.Check(t, dir, r)
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input struct {
				Args  []string
				Files map[string]string
				Env   []string
			}
			Expect struct {
				Code   int
				Stderr []string
			}
			Test struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		usage := []string{"usage: xmrgateway <command> [flags]", "daemon", "install", "server", "deploy", "token", "pow ", "prices", "check", "schema", "version"}
		tests := []Test{
			{Name: "no command", Input: Input{}, Expect: Expect{2, usage}},
			{Name: "unknown command", Input: Input{Args: []string{"serve"}}, Expect: Expect{2, usage}},
			{Name: "unknown flag", Input: Input{Args: []string{"version", "-x"}}, Expect: Expect{2, []string{"flag provided but not defined: -x"}}},
			{Name: "pow without arguments", Input: Input{Args: []string{"pow"}}, Expect: Expect{1, []string{"pow: PAYLOAD and DIFFICULTY are required", "usage: xmrgateway pow"}}},
			{Name: "pow without difficulty", Input: Input{Args: []string{"pow", "p"}}, Expect: Expect{1, []string{"PAYLOAD and DIFFICULTY are required"}}},
			{Name: "pow with extra argument", Input: Input{Args: []string{"pow", "p", "8", "x"}}, Expect: Expect{1, []string{"PAYLOAD and DIFFICULTY are required"}}},
			{Name: "pow difficulty zero", Input: Input{Args: []string{"pow", "p", "0"}}, Expect: Expect{1, []string{`DIFFICULTY must be a number between 1 and 64, got "0"`}}},
			{Name: "pow difficulty too high", Input: Input{Args: []string{"pow", "p", "65"}}, Expect: Expect{1, []string{`got "65"`}}},
			{Name: "pow difficulty not a number", Input: Input{Args: []string{"pow", "p", "lots"}}, Expect: Expect{1, []string{`got "lots"`}}},
			{Name: "pow bad -until", Input: Input{Args: []string{"pow", "-until", "tomorrow", "p", "8"}}, Expect: Expect{1, []string{"pow: -until: parsing time"}}},
			{Name: "check missing file", Input: Input{Args: []string{"check"}}, Expect: Expect{1, []string{"check: open config.json: no such file"}}},
			{
				Name:   "check invalid config",
				Input:  Input{Args: []string{"check"}, Files: map[string]string{"config.json": `{"secret":"short","pow_difficulty":99}`}},
				Expect: Expect{1, []string{"check: config.json:\n", "secret must be at least 32 characters", "pow_difficulty must be between 8 and 40"}},
			},
			{
				Name:   "check unknown field",
				Input:  Input{Args: []string{"check"}, Files: map[string]string{"config.json": `{"sekret":"x"}`}},
				Expect: Expect{1, []string{`unknown field "sekret"`}},
			},
			{Name: "home that does not exist", Input: Input{Args: []string{"check", "-chdir-home"}, Env: []string{"HOME={dir}/absent"}}, Expect: Expect{1, []string{"check: chdir"}}},
			{
				Name:   "server with an invalid config",
				Input:  Input{Args: []string{"server"}, Files: map[string]string{"config.json": `{"secret":"short"}`}},
				Expect: Expect{1, []string{"server: config.json:\n", "secret must be at least 32 characters"}},
			},
			{Name: "daemon without a config", Input: Input{Args: []string{"daemon"}}, Expect: Expect{1, []string{"daemon: open config.json"}}},
			{Name: "install for real off OpenBSD", Input: Input{Args: []string{"install"}}, Expect: Expect{1, []string{"install: install only supports OpenBSD, use -dry-run to see the steps"}}},
			{Name: "deploy onto a file", Input: Input{Args: []string{"deploy", "-dir", "f"}, Files: map[string]string{"f": ""}}, Expect: Expect{1, []string{"deploy: ", "not a directory"}}},
			{Name: "token with a bad proxy", Input: Input{Args: []string{"token", "-proxy", "http://[::1"}}, Expect: Expect{1, []string{"token: ", "missing ']'"}}},
			{Name: "token from a dead gateway", Input: Input{Args: []string{"token", "-url", "http://127.0.0.1:1", "-timeout", "5s"}}, Expect: Expect{1, []string{"token: ", "connection refused"}}},
			{Name: "prices with a bad proxy", Input: Input{Args: []string{"prices", "-proxy", "http://[::1"}}, Expect: Expect{1, []string{"prices: ", "missing ']'"}}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				_, r := invoke(t, test.Input.Files, test.Input.Env, test.Input.Args)
				if r.Code != test.Expect.Code {
					t.Fatalf("exit %d, want %d\nstderr: %s", r.Code, test.Expect.Code, r.Stderr)
				}
				for _, want := range test.Expect.Stderr {
					if !strings.Contains(r.Stderr, want) {
						t.Errorf("stderr is missing %q:\n%s", want, r.Stderr)
					}
				}
			})
		}
	})
}

func mustAbs(t *testing.T, p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

// TestServe starts server and daemon as real processes, talks to them and
// stops them with a signal.
func TestServe(t *testing.T) {
	addrRe := regexp.MustCompile(`\.addr=(\S+)`)

	t.Run("Succeed", func(t *testing.T) {
		type (
			Input struct {
				Command string
				Signal  syscall.Signal
			}
			Expect struct {
				Timestamps bool
				Group      string
			}
			Test struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "server stops on SIGTERM", Input: Input{"server", syscall.SIGTERM}, Expect: Expect{true, "server."}},
			{Name: "server stops on interrupt", Input: Input{"server", syscall.SIGINT}, Expect: Expect{true, "server."}},
			{Name: "daemon logs without time", Input: Input{"daemon", syscall.SIGTERM}, Expect: Expect{false, "daemon."}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				dir := t.TempDir()
				writeFiles(t, dir, map[string]string{"config.json": validConfig(nil)})
				cmd := process(t, dir, nil, test.Input.Command)
				stderr, _ := cmd.StderrPipe()
				if err := cmd.Start(); err != nil {
					t.Fatal(err)
				}

				lines := make(chan string, 100)
				go func() {
					sc := bufio.NewScanner(stderr)
					for sc.Scan() {
						lines <- sc.Text()
					}
					close(lines)
				}()
				var log []string
				var addr string
				deadline := time.After(10 * time.Second)
				for addr == "" {
					select {
					case line, ok := <-lines:
						if !ok {
							t.Fatalf("exited before listening:\n%s", strings.Join(log, "\n"))
						}
						log = append(log, line)
						if m := addrRe.FindStringSubmatch(line); m != nil && strings.Contains(line, "msg=listening") {
							addr = m[1]
						}
					case <-deadline:
						t.Fatalf("never listened:\n%s", strings.Join(log, "\n"))
					}
				}

				resp, err := http.Get("http://" + addr + "/")
				if err != nil {
					t.Fatal(err)
				}
				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "Generate token") {
					t.Fatalf("home: %d", resp.StatusCode)
				}

				cmd.Process.Signal(test.Input.Signal)
				for line := range lines {
					log = append(log, line)
				}
				if err := cmd.Wait(); err != nil {
					t.Fatalf("exit: %v\n%s", err, strings.Join(log, "\n"))
				}
				all := strings.Join(log, "\n")
				if !strings.Contains(all, "msg=\"shutting down\"") || !strings.Contains(all, test.Expect.Group+"PriceWorker") {
					t.Errorf("missing shutdown or worker logs:\n%s", all)
				}
				if strings.Contains(all, "time=") != test.Expect.Timestamps {
					t.Errorf("timestamps = %v:\n%s", !test.Expect.Timestamps, all)
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input  struct{ Listen func(t *testing.T) string }
			Expect struct{ Stderr string }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{
				Name: "port in use",
				Input: Input{func(t *testing.T) string {
					ln, err := net.Listen("tcp", "127.0.0.1:0")
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { ln.Close() })
					return ln.Addr().String()
				}},
				Expect: Expect{"address already in use"},
			},
			{Name: "address that cannot be bound", Input: Input{func(*testing.T) string { return "256.0.0.1:80" }}, Expect: Expect{"server: listen tcp"}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				dir := t.TempDir()
				listen := test.Input.Listen(t)
				writeFiles(t, dir, map[string]string{"config.json": validConfig(func(c *config.Config) { c.Listen = listen })})
				r := cli(t, dir, nil, "server")
				if r.Code != 1 || !strings.Contains(r.Stderr, test.Expect.Stderr) {
					t.Fatalf("exit %d:\n%s", r.Code, r.Stderr)
				}
			})
		}
	})
}
