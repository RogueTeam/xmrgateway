package main

import (
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// capture swaps f (os.Stdout or os.Stderr) for a pipe while fn runs.
func capture(t *testing.T, f **os.File, fn func() error) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := *f
	*f = w
	out := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		out <- string(b)
	}()
	err = fn()
	*f = old
	w.Close()
	return <-out, err
}

func TestParse(t *testing.T) {
	// setup makes dir/start (the working directory) and dir/home, points HOME
	// at home(dir) and returns dir.
	setup := func(t *testing.T, home func(dir string) string) string {
		dir, _ := filepath.EvalSymlinks(t.TempDir())
		os.Mkdir(filepath.Join(dir, "start"), 0o755)
		os.Mkdir(filepath.Join(dir, "home"), 0o755)
		t.Chdir(filepath.Join(dir, "start"))
		t.Setenv("HOME", home(dir))
		return dir
	}
	homeDir := func(dir string) string { return filepath.Join(dir, "home") }
	startDir := func(dir string) string { return filepath.Join(dir, "start") }
	absent := func(dir string) string { return filepath.Join(dir, "absent") }

	t.Run("Succeed", func(t *testing.T) {
		type (
			Input struct {
				Args []string
				Home func(dir string) string
			}
			Expect struct {
				Dir    func(dir string) string
				Config string
			}
			Test struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "stays put by default", Input: Input{nil, homeDir}, Expect: Expect{startDir, "config.json"}},
			{Name: "moves home", Input: Input{[]string{"-chdir-home"}, homeDir}, Expect: Expect{homeDir, "config.json"}},
			{Name: "with other flags", Input: Input{[]string{"-config", "x.json", "-chdir-home"}, homeDir}, Expect: Expect{homeDir, "x.json"}},
			{Name: "explicit false", Input: Input{[]string{"-chdir-home=false"}, absent}, Expect: Expect{startDir, "config.json"}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				dir := setup(t, test.Input.Home)
				fs, home := newFlags("test")
				config := fs.String("config", "config.json", "")
				if err := parse(fs, home, test.Input.Args); err != nil {
					t.Fatal(err)
				}
				if wd, _ := os.Getwd(); wd != test.Expect.Dir(dir) {
					t.Fatalf("working directory %s, want %s", wd, test.Expect.Dir(dir))
				}
				if *config != test.Expect.Config {
					t.Fatalf("config %q", *config)
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input struct {
				Args []string
				Home func(dir string) string
			}
			Expect struct{ ErrContains string }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "home does not exist", Input: Input{[]string{"-chdir-home"}, absent}, Expect: Expect{"no such file"}},
			{Name: "home is not set", Input: Input{[]string{"-chdir-home"}, func(string) string { return "" }}, Expect: Expect{"$HOME is not defined"}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				dir := setup(t, test.Input.Home)
				fs, home := newFlags("test")
				if err := parse(fs, home, test.Input.Args); err == nil || !strings.Contains(err.Error(), test.Expect.ErrContains) {
					t.Fatalf("got %v, want %q", err, test.Expect.ErrContains)
				}
				if wd, _ := os.Getwd(); wd != startDir(dir) {
					t.Fatalf("moved to %s on error", wd)
				}
			})
		}
	})
}

func TestHTTPClient(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input struct {
				Proxy   string
				Timeout time.Duration
			}
			Expect struct{ Proxy string }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "tor", Input: Input{"socks5h://127.0.0.1:9050", time.Minute}, Expect: Expect{"socks5h://127.0.0.1:9050"}},
			{Name: "http proxy", Input: Input{"http://proxy:3128", 5 * time.Second}, Expect: Expect{"http://proxy:3128"}},
			{Name: "no proxy", Input: Input{"", 30 * time.Second}, Expect: Expect{""}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				c, err := httpClient(test.Input.Proxy, test.Input.Timeout)
				if err != nil {
					t.Fatal(err)
				}
				if c.Timeout != test.Input.Timeout {
					t.Errorf("timeout %v", c.Timeout)
				}
				req, _ := http.NewRequest(http.MethodGet, "https://api.kraken.com/", nil)
				// Without -proxy the environment decides, as for any Go program.
				want := test.Expect.Proxy
				if want == "" {
					if env, _ := http.ProxyFromEnvironment(req); env != nil {
						want = env.String()
					}
				}
				u, err := c.Transport.(*http.Transport).Proxy(req)
				got := ""
				if u != nil {
					got = u.String()
				}
				if err != nil || got != want {
					t.Fatalf("proxy %q, want %q, %v", got, want, err)
				}
				if c.Transport == http.DefaultTransport {
					t.Fatal("shares the default transport")
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input  struct{ Proxy string }
			Expect struct{ ErrContains string }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "unclosed ipv6", Input: Input{"http://[::1"}, Expect: Expect{"missing ']'"}},
			{Name: "control character", Input: Input{"http://proxy\x7f:1"}, Expect: Expect{"invalid control character"}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				c, err := httpClient(test.Input.Proxy, time.Second)
				if err == nil || c != nil || !strings.Contains(err.Error(), test.Expect.ErrContains) {
					t.Fatalf("got %v, %v", c, err)
				}
			})
		}
	})
}

func TestNewLogger(t *testing.T) {
	logs := func(t *testing.T, level string, timestamps bool, log func(l *slog.Logger)) string {
		out, _ := capture(t, &os.Stderr, func() error {
			log(newLogger(level, timestamps))
			return nil
		})
		return out
	}

	t.Run("Succeed", func(t *testing.T) {
		type (
			Input struct {
				Level      string
				Timestamps bool
				Log        func(l *slog.Logger)
			}
			Expect struct{ Contains, Absent []string }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{
				Name:   "server keeps timestamps",
				Input:  Input{"info", true, func(l *slog.Logger) { l.Info("hi", "k", 1) }},
				Expect: Expect{Contains: []string{"time=", "level=INFO", "msg=hi", "k=1"}},
			},
			{
				Name:   "daemon leaves time to syslog",
				Input:  Input{"info", false, func(l *slog.Logger) { l.Info("hi") }},
				Expect: Expect{Contains: []string{"level=INFO msg=hi"}, Absent: []string{"time="}},
			},
			{
				Name:   "time inside a group is kept",
				Input:  Input{"info", false, func(l *slog.Logger) { l.WithGroup("g").Info("hi", "time", "noon") }},
				Expect: Expect{Contains: []string{"g.time=noon"}},
			},
			{
				Name:   "debug",
				Input:  Input{"debug", true, func(l *slog.Logger) { l.Debug("details") }},
				Expect: Expect{Contains: []string{"level=DEBUG msg=details"}},
			},
			{
				Name:   "warn hides info",
				Input:  Input{"warn", true, func(l *slog.Logger) { l.Info("quiet"); l.Warn("loud") }},
				Expect: Expect{Contains: []string{"msg=loud"}, Absent: []string{"quiet"}},
			},
			{
				Name:   "error hides warn",
				Input:  Input{"error", true, func(l *slog.Logger) { l.Warn("quiet"); l.Error("loud") }},
				Expect: Expect{Contains: []string{"msg=loud"}, Absent: []string{"quiet"}},
			},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				out := logs(t, test.Input.Level, test.Input.Timestamps, test.Input.Log)
				for _, want := range test.Expect.Contains {
					if !strings.Contains(out, want) {
						t.Errorf("missing %q in %q", want, out)
					}
				}
				for _, absent := range test.Expect.Absent {
					if strings.Contains(out, absent) {
						t.Errorf("has %q in %q", absent, out)
					}
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input  struct{ Level string }
			Expect struct{ Shown, Hidden string }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		// A level that does not parse leaves the logger at info.
		tests := []Test{
			{Name: "unknown level", Input: Input{"loud"}, Expect: Expect{"msg=shown", "hidden"}},
			{Name: "empty level", Input: Input{""}, Expect: Expect{"msg=shown", "hidden"}},
			{Name: "upper case is not a level name", Input: Input{"DEBUGGING"}, Expect: Expect{"msg=shown", "hidden"}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				out := logs(t, test.Input.Level, true, func(l *slog.Logger) { l.Debug("hidden"); l.Info("shown") })
				if !strings.Contains(out, test.Expect.Shown) || strings.Contains(out, test.Expect.Hidden) {
					t.Fatalf("got %q", out)
				}
			})
		}
	})
}

func TestCurrentVersion(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input  struct{ Version string }
			Expect struct{ Version string }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "set at build time", Input: Input{"v1.2.3"}, Expect: Expect{"v1.2.3"}},
			{Name: "dev build", Input: Input{"dev"}, Expect: Expect{"dev"}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				old := version
				t.Cleanup(func() { version = old })
				version = test.Input.Version
				if got := currentVersion(); got != test.Expect.Version {
					t.Fatalf("got %q", got)
				}
			})
		}
	})
}

// TestCommands calls the command functions directly; TestCLI covers the
// same commands as a process, with exit codes.
func TestCommands(t *testing.T) {
	// call runs a command function in a fresh directory holding files.
	call := func(t *testing.T, run func([]string) error, args []string, files map[string]string) (string, error) {
		dir := t.TempDir()
		writeFiles(t, dir, files)
		t.Chdir(dir)
		return capture(t, &os.Stdout, func() error { return run(args) })
	}

	t.Run("Succeed", func(t *testing.T) {
		type (
			Input struct {
				Run   func(args []string) error
				Args  []string
				Files map[string]string
			}
			Expect struct{ Stdout []string }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "version", Input: Input{Run: versionCmd}, Expect: Expect{[]string{"dev\n"}}},
			{Name: "schema", Input: Input{Run: schemaCmd}, Expect: Expect{[]string{`"title": "xmrgateway configuration"`}}},
			{Name: "check", Input: Input{Run: checkCmd, Files: map[string]string{"config.json": validConfig(nil)}}, Expect: Expect{[]string{"config.json is valid"}}},
			{Name: "deploy", Input: Input{Run: deployCmd, Args: []string{"-dir", "s"}}, Expect: Expect{[]string{"static assets written to s"}}},
			{Name: "install dry run", Input: Input{Run: installCmd, Args: []string{"-dry-run"}}, Expect: Expect{[]string{"6. write /etc/rc.d/xmrgateway"}}},
			{Name: "pow", Input: Input{Run: powCmd, Args: []string{"p", "4"}}, Expect: Expect{[]string{"1:4:", ":p::"}}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				out, err := call(t, test.Input.Run, test.Input.Args, test.Input.Files)
				if err != nil {
					t.Fatal(err)
				}
				for _, want := range test.Expect.Stdout {
					if !strings.Contains(out, want) {
						t.Errorf("stdout is missing %q:\n%s", want, out)
					}
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input struct {
				Run   func(args []string) error
				Args  []string
				Files map[string]string
			}
			Expect struct{ ErrContains string }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "check missing", Input: Input{Run: checkCmd}, Expect: Expect{"no such file"}},
			{Name: "check invalid", Input: Input{Run: checkCmd, Files: map[string]string{"config.json": `{}`}}, Expect: Expect{"secret must be at least 32 characters"}},
			{Name: "deploy onto a file", Input: Input{Run: deployCmd, Args: []string{"-dir", "f"}, Files: map[string]string{"f": ""}}, Expect: Expect{"not a directory"}},
			{Name: "install for real", Input: Input{Run: installCmd}, Expect: Expect{"only supports OpenBSD"}},
			{Name: "pow bad difficulty", Input: Input{Run: powCmd, Args: []string{"p", "x"}}, Expect: Expect{"DIFFICULTY must be a number"}},
			{Name: "serve invalid", Input: Input{Run: serverCmd, Files: map[string]string{"config.json": `{}`}}, Expect: Expect{"secret must be"}},
			{Name: "token bad proxy", Input: Input{Run: tokenCmd, Args: []string{"-proxy", "http://[::1"}}, Expect: Expect{"missing ']'"}},
			{Name: "prices bad proxy", Input: Input{Run: pricesCmd, Args: []string{"-proxy", "http://[::1"}}, Expect: Expect{"missing ']'"}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				if _, err := call(t, test.Input.Run, test.Input.Args, test.Input.Files); err == nil || !strings.Contains(err.Error(), test.Expect.ErrContains) {
					t.Fatalf("got %v, want %q", err, test.Expect.ErrContains)
				}
			})
		}
	})
}

func TestLockdown(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input  struct{ StaticDir string }
			Expect struct{}
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "existing static dir", Input: Input{t.TempDir()}},
			{Name: "missing static dir is fine", Input: Input{filepath.Join(t.TempDir(), "absent")}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				// Off OpenBSD the sandbox calls are no-ops, so this only checks
				// the preloading and the error handling around them.
				if err := lockdown(test.Input.StaticDir); err != nil {
					t.Fatal(err)
				}
			})
		}
	})
}
