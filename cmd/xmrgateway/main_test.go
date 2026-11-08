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
	"strconv"
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
		// Children started by the daemon command inherit the variable but
		// carry their own arguments.
		args := os.Args[1:]
		if len(args) == 0 {
			if err := json.Unmarshal([]byte(raw), &args); err != nil {
				panic(err)
			}
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
			{Name: "daemon help", Input: Input{Args: []string{"daemon", "-h"}}, Expect: Expect{Stderr: []string{"-restart-interval", "-config", "-chdir-home"}}},
			{Name: "server help", Input: Input{Args: []string{"server", "-h"}}, Expect: Expect{Stderr: []string{"-log-time"}}},
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
			{
				Name:   "daemon with an invalid config",
				Input:  Input{Args: []string{"daemon"}, Files: map[string]string{"config.json": `{"secret":"short"}`}},
				Expect: Expect{1, []string{"daemon: config.json:\n", "secret must be at least 32 characters"}},
			},
			{Name: "daemon with a bad interval", Input: Input{Args: []string{"daemon", "-restart-interval", "soon"}}, Expect: Expect{2, []string{"invalid value \"soon\" for flag -restart-interval"}}},
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
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input struct {
				Args       []string
				KillServer bool
				Signal     syscall.Signal
			}
			Expect struct {
				Timestamps bool
				Starts     int
			}
			Test struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "server stops on SIGTERM", Input: Input{Args: []string{"server"}, Signal: syscall.SIGTERM}, Expect: Expect{true, 1}},
			{Name: "server stops on interrupt", Input: Input{Args: []string{"server"}, Signal: syscall.SIGINT}, Expect: Expect{true, 1}},
			{Name: "server without time", Input: Input{Args: []string{"server", "-log-time=false"}, Signal: syscall.SIGTERM}, Expect: Expect{false, 1}},
			{Name: "daemon stops its server on SIGTERM", Input: Input{Args: []string{"daemon"}, Signal: syscall.SIGTERM}, Expect: Expect{false, 1}},
			{Name: "daemon stops its server on interrupt", Input: Input{Args: []string{"daemon"}, Signal: syscall.SIGINT}, Expect: Expect{false, 1}},
			{
				Name:   "daemon restarts a killed server",
				Input:  Input{Args: []string{"daemon", "-restart-interval", "2s"}, KillServer: true, Signal: syscall.SIGTERM},
				Expect: Expect{false, 2},
			},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				dir := t.TempDir()
				writeFiles(t, dir, map[string]string{"config.json": validConfig(nil)})
				s := launch(t, dir, test.Input.Args...)
				addr, pid := s.listening(t)
				home(t, addr)
				if test.Input.KillServer {
					syscall.Kill(pid, syscall.SIGKILL)
					again, newPid := s.listening(t)
					if newPid == pid {
						t.Fatal("same server pid after the restart")
					}
					home(t, again)
				}

				code, all := s.finish(t, test.Input.Signal)
				if code != 0 {
					t.Fatalf("exit %d:\n%s", code, all)
				}
				if !strings.Contains(all, `msg="shutting down"`) || !strings.Contains(all, "server.PriceWorker") {
					t.Errorf("missing shutdown or worker logs:\n%s", all)
				}
				if timeAttr.MatchString(all) != test.Expect.Timestamps {
					t.Errorf("timestamps = %v:\n%s", !test.Expect.Timestamps, all)
				}
				if n := strings.Count(all, "msg=listening"); n != test.Expect.Starts {
					t.Errorf("server started %d times, want %d", n, test.Expect.Starts)
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
			{Name: "port in use", Input: Input{busyPort}, Expect: Expect{"address already in use"}},
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

	t.Run("Fail under daemon", func(t *testing.T) {
		type (
			Input  struct{ Listen func(t *testing.T) string }
			Expect struct {
				Stderr   string
				Attempts int
			}
			Test struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "keeps retrying a server that cannot listen", Input: Input{busyPort}, Expect: Expect{"address already in use", 2}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				dir := t.TempDir()
				listen := test.Input.Listen(t)
				writeFiles(t, dir, map[string]string{"config.json": validConfig(func(c *config.Config) { c.Listen = listen })})
				s := launch(t, dir, "daemon", "-restart-interval", "2s")
				for range test.Expect.Attempts {
					s.until(t, test.Expect.Stderr)
				}
				if code, all := s.finish(t, syscall.SIGTERM); code != 0 {
					t.Fatalf("exit %d:\n%s", code, all)
				}
			})
		}
	})
}

func busyPort(t *testing.T) string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	return ln.Addr().String()
}

func home(t *testing.T, addr string) {
	t.Helper()
	resp, err := http.Get("http://" + addr + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "Generate token") {
		t.Fatalf("home: %d", resp.StatusCode)
	}
}

// session is a running command whose stderr is read line by line.
type session struct {
	cmd   *exec.Cmd
	lines chan string
	log   []string
}

func launch(t *testing.T, dir string, args ...string) *session {
	t.Helper()
	cmd := process(t, dir, nil, args...)
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	s := &session{cmd: cmd, lines: make(chan string, 1000)}
	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			s.lines <- sc.Text()
		}
		close(s.lines)
	}()
	// The pipe only closes once every process holding it exited, so this
	// also waits for children left behind.
	t.Cleanup(func() {
		cmd.Process.Kill()
		for range s.lines {
		}
		cmd.Wait()
	})
	return s
}

// until reads lines until one contains substr and returns it.
func (s *session) until(t *testing.T, substr string) string {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case line, ok := <-s.lines:
			if !ok {
				t.Fatalf("exited before %q:\n%s", substr, strings.Join(s.log, "\n"))
			}
			s.log = append(s.log, line)
			if strings.Contains(line, substr) {
				return line
			}
		case <-deadline:
			t.Fatalf("no %q:\n%s", substr, strings.Join(s.log, "\n"))
		}
	}
}

var timeAttr = regexp.MustCompile(`(?m)^time=\d{4}-`)

var listenRe = regexp.MustCompile(`\.addr=(\S+) server\.pid=(\d+)`)

func (s *session) listening(t *testing.T) (addr string, pid int) {
	t.Helper()
	m := listenRe.FindStringSubmatch(s.until(t, "msg=listening"))
	if m == nil {
		t.Fatalf("listening line without addr and pid:\n%s", strings.Join(s.log, "\n"))
	}
	pid, _ = strconv.Atoi(m[2])
	return m[1], pid
}

// finish signals the command, reads the rest of its output and returns its
// exit code with everything it logged.
func (s *session) finish(t *testing.T, sig syscall.Signal) (int, string) {
	t.Helper()
	s.cmd.Process.Signal(sig)
	deadline := time.After(20 * time.Second)
	for done := false; !done; {
		select {
		case line, ok := <-s.lines:
			if !ok {
				done = true
				break
			}
			s.log = append(s.log, line)
		case <-deadline:
			t.Fatalf("did not exit, or left a child holding stderr:\n%s", strings.Join(s.log, "\n"))
		}
	}
	err := s.cmd.Wait()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), strings.Join(s.log, "\n")
	}
	if err != nil {
		t.Fatal(err)
	}
	return 0, strings.Join(s.log, "\n")
}
