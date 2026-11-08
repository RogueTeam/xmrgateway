package install

import (
	"encoding/json"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RogueTeam/xmrgateway/internal/config"
)

func custom() Options {
	return Options{Binary: "/opt/bin/xmrgw", User: "_prices", UID: 6000, Home: "/var/prices", Datasize: "1024M", OpenFiles: 512}
}

func TestDefaults(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input  struct{}
			Expect struct{ Options Options }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "openbsd layout", Expect: Expect{Options{"/usr/local/bin/xmrgateway", "_xmrgateway", 5000, "/home/_xmrgateway", "4096M", 4096}}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				if got := Defaults(); got != test.Expect.Options {
					t.Fatalf("got %+v", got)
				}
			})
		}
	})
}

func TestRCScript(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input  struct{ Options Options }
			Expect struct{ Contains, Absent []string }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{
				Name:  "defaults",
				Input: Input{Defaults()},
				Expect: Expect{
					Contains: []string{
						"#!/bin/ksh\n",
						`daemon="/usr/local/bin/xmrgateway"`,
						`daemon_flags="daemon -chdir-home -config config.json"`,
						`daemon_user="_xmrgateway"`,
						`daemon_logger="daemon.info"`,
						". /etc/rc.d/rc.subr\n\nrc_bg=YES\nrc_reload=NO\n\nrc_cmd $1\n",
					},
					Absent: []string{"daemon_class", "daemon_timeout"},
				},
			},
			{
				Name:   "custom binary and user",
				Input:  Input{custom()},
				Expect: Expect{Contains: []string{`daemon="/opt/bin/xmrgw"`, `daemon_user="_prices"`}, Absent: []string{"_xmrgateway"}},
			},
			{
				Name:   "quotes are escaped",
				Input:  Input{Options{Binary: `/opt/x"y`, User: "u"}},
				Expect: Expect{Contains: []string{`daemon="/opt/x\"y"`}},
			},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				s := RCScript(test.Input.Options)
				for _, want := range test.Expect.Contains {
					if !strings.Contains(s, want) {
						t.Errorf("missing %q in:\n%s", want, s)
					}
				}
				for _, absent := range test.Expect.Absent {
					if strings.Contains(s, absent) {
						t.Errorf("has %q", absent)
					}
				}
			})
		}
	})
}

func TestLoginClass(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input  struct{ Options Options }
			Expect struct{ Class string }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "defaults", Input: Input{Defaults()}, Expect: Expect{"xmrgateway:\\\n\t:datasize=4096M:\\\n\t:openfiles=4096:\\\n\t:tc=daemon:\n"}},
			{Name: "custom limits", Input: Input{custom()}, Expect: Expect{"xmrgateway:\\\n\t:datasize=1024M:\\\n\t:openfiles=512:\\\n\t:tc=daemon:\n"}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				if got := LoginClass(test.Input.Options); got != test.Expect.Class {
					t.Fatalf("got\n%s\nwant\n%s", got, test.Expect.Class)
				}
			})
		}
	})
}

func TestNewConfig(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input  struct{ Runs int }
			Expect struct {
				SecretLen int
				RateLimit time.Duration
			}
			Test struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "fresh secrets with defaults", Input: Input{20}, Expect: Expect{64, 30 * time.Second}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				seen := map[string]bool{}
				for range test.Input.Runs {
					cfg, err := NewConfig()
					if err != nil {
						t.Fatal(err)
					}
					if len(cfg.Secret) != test.Expect.SecretLen || strings.Trim(cfg.Secret, "0123456789abcdef") != "" || seen[cfg.Secret] {
						t.Fatalf("secret %q", cfg.Secret)
					}
					seen[cfg.Secret] = true
					if cfg.RateLimit.Duration != test.Expect.RateLimit {
						t.Fatalf("defaults not applied: %+v", cfg)
					}
				}
			})
		}
	})
}

func TestSteps(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input struct {
				Options Options
				Self    string
			}
			Expect struct{ Descriptions []string }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{
				Name:  "defaults",
				Input: Input{Defaults(), "/tmp/xmrgateway"},
				Expect: Expect{[]string{
					"copy /tmp/xmrgateway to /usr/local/bin/xmrgateway",
					"write login class /etc/login.conf.d/xmrgateway",
					"create user _xmrgateway with uid 5000, class xmrgateway and home /home/_xmrgateway",
					"write /home/_xmrgateway/config.json with a random secret, unless it exists",
					"deploy static assets to /home/_xmrgateway/static",
					"write /etc/rc.d/xmrgateway",
				}},
			},
			{
				Name:  "custom",
				Input: Input{custom(), "/root/go/bin/xmrgateway"},
				Expect: Expect{[]string{
					"copy /root/go/bin/xmrgateway to /opt/bin/xmrgw",
					"write login class /etc/login.conf.d/xmrgateway",
					"create user _prices with uid 6000, class xmrgateway and home /var/prices",
					"write /var/prices/config.json with a random secret, unless it exists",
					"deploy static assets to /var/prices/static",
					"write /etc/rc.d/xmrgateway",
				}},
			},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				steps := Steps(test.Input.Options, test.Input.Self)
				if len(steps) != len(test.Expect.Descriptions) {
					t.Fatalf("%d steps", len(steps))
				}
				for i, s := range steps {
					if s.Description != test.Expect.Descriptions[i] || s.Run == nil {
						t.Errorf("step %d: %q", i+1, s.Description)
					}
				}
			})
		}
	})
}

// TestStepsRun runs the steps that only touch the home directory and the
// binary path, pointed at a temporary directory and the current user.
func TestStepsRun(t *testing.T) {
	me, err := user.Current()
	if err != nil {
		t.Skip(err)
	}
	const (
		stepCopy   = 0
		stepConfig = 3
		stepStatic = 4
	)
	// layout makes dir/self, dir/home and options for the current user,
	// lets setup adjust them, and returns the chosen step.
	layout := func(t *testing.T, step int, setup func(o *Options)) (dir string, s Step) {
		dir = t.TempDir()
		o := Options{Binary: filepath.Join(dir, "bin", "xmrgateway"), User: me.Username, Home: filepath.Join(dir, "home")}
		os.Mkdir(o.Home, 0o755)
		self := filepath.Join(dir, "self")
		os.WriteFile(self, []byte("binary"), 0o755)
		if setup != nil {
			setup(&o)
		}
		return dir, Steps(o, self)[step]
	}

	t.Run("Succeed", func(t *testing.T) {
		type (
			Input struct {
				Step  int
				Twice bool
			}
			Expect struct{ Files []string }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "copy the binary", Input: Input{stepCopy, false}, Expect: Expect{[]string{"bin/xmrgateway"}}},
			{Name: "copy again over the old one", Input: Input{stepCopy, true}, Expect: Expect{[]string{"bin/xmrgateway"}}},
			{Name: "write the config", Input: Input{stepConfig, false}, Expect: Expect{[]string{"home/config.json"}}},
			{Name: "deploy static files twice", Input: Input{stepStatic, true}, Expect: Expect{[]string{"home/static/pico.orange.min.css", "home/static/logo.svg"}}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				dir, step := layout(t, test.Input.Step, nil)
				if err := step.Run(); err != nil {
					t.Fatal(err)
				}
				if test.Input.Twice {
					if err := step.Run(); err != nil {
						t.Fatalf("second run: %v", err)
					}
				}
				for _, f := range test.Expect.Files {
					if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
						t.Errorf("%s: %v", f, err)
					}
				}
			})
		}
	})

	t.Run("Succeed with the config", func(t *testing.T) {
		type (
			Input  struct{ Existing string }
			Expect struct{ Keep bool }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "written config is private and valid", Input: Input{""}, Expect: Expect{false}},
			{Name: "existing config is kept", Input: Input{`{"secret":"mine"}`}, Expect: Expect{true}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				dir, step := layout(t, stepConfig, nil)
				path := filepath.Join(dir, "home", "config.json")
				if test.Input.Existing != "" {
					os.WriteFile(path, []byte(test.Input.Existing), 0o600)
				}
				if err := step.Run(); err != nil {
					t.Fatal(err)
				}
				b, _ := os.ReadFile(path)
				if test.Expect.Keep {
					if string(b) != test.Input.Existing {
						t.Fatalf("config overwritten: %s", b)
					}
					return
				}
				if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
					t.Errorf("mode %v", info.Mode())
				}
				cfg, err := config.Load(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := cfg.Validate(); err != nil {
					t.Fatal(err)
				}
				var raw map[string]any
				json.Unmarshal(b, &raw)
				if raw["token_validity"] != "2160h0m0s" {
					t.Errorf("token_validity %v", raw["token_validity"])
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input struct {
				Step  int
				Setup func(o *Options)
			}
			Expect struct{ ErrContains string }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{
				Name:   "missing binary",
				Input:  Input{Step: stepCopy, Setup: func(o *Options) { os.Remove(filepath.Join(filepath.Dir(filepath.Dir(o.Binary)), "self")) }},
				Expect: Expect{"no such file"},
			},
			{
				Name:   "home missing",
				Input:  Input{Step: stepConfig, Setup: func(o *Options) { o.Home = filepath.Join(o.Home, "absent") }},
				Expect: Expect{"no such file"},
			},
			{
				Name:   "unknown user",
				Input:  Input{Step: stepConfig, Setup: func(o *Options) { o.User = "_no_such_user_here" }},
				Expect: Expect{"unknown user"},
			},
			{
				Name:   "static path is a file",
				Input:  Input{Step: stepStatic, Setup: func(o *Options) { os.WriteFile(filepath.Join(o.Home, "static"), nil, 0o644) }},
				Expect: Expect{"not a directory"},
			},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				_, step := layout(t, test.Input.Step, test.Input.Setup)
				if err := step.Run(); err == nil || !strings.Contains(err.Error(), test.Expect.ErrContains) {
					t.Fatalf("got %v, want %q", err, test.Expect.ErrContains)
				}
			})
		}
	})
}

func TestCopyFile(t *testing.T) {
	file := func(t *testing.T, path, content string) string {
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
		return path
	}

	t.Run("Succeed", func(t *testing.T) {
		type (
			Input struct {
				Setup func(t *testing.T, dir string) (src, dst string)
			}
			Expect struct{ Content string }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{
				Name: "into a new directory",
				Input: Input{func(t *testing.T, dir string) (string, string) {
					return file(t, filepath.Join(dir, "src"), "binary"), filepath.Join(dir, "bin", "dst")
				}},
				Expect: Expect{"binary"},
			},
			{
				Name: "replaces an existing file",
				Input: Input{func(t *testing.T, dir string) (string, string) {
					dst := file(t, filepath.Join(dir, "dst"), "old")
					os.Chmod(dst, 0o444)
					return file(t, filepath.Join(dir, "src"), "new"), dst
				}},
				Expect: Expect{"new"},
			},
			{
				Name: "onto itself",
				Input: Input{func(t *testing.T, dir string) (string, string) {
					p := file(t, filepath.Join(dir, "same"), "binary")
					os.Chmod(p, 0o555)
					return p, p
				}},
				Expect: Expect{"binary"},
			},
			{
				Name: "through a symlink to the destination",
				Input: Input{func(t *testing.T, dir string) (string, string) {
					dst := file(t, filepath.Join(dir, "real"), "binary")
					os.Chmod(dst, 0o555)
					link := filepath.Join(dir, "link")
					os.Symlink(dst, link)
					return link, dst
				}},
				Expect: Expect{"binary"},
			},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				src, dst := test.Input.Setup(t, t.TempDir())
				if err := copyFile(src, dst, 0o555); err != nil {
					t.Fatal(err)
				}
				b, _ := os.ReadFile(dst)
				info, _ := os.Stat(dst)
				if string(b) != test.Expect.Content || info.Mode().Perm() != 0o555 {
					t.Fatalf("content %q mode %v", b, info.Mode())
				}
				if _, err := os.Stat(dst + ".new"); err == nil {
					t.Fatal("temporary file left behind")
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input struct {
				Setup func(t *testing.T, dir string) (src, dst string)
			}
			Expect struct{ ErrContains string }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{
				Name: "missing source",
				Input: Input{func(t *testing.T, dir string) (string, string) {
					return filepath.Join(dir, "absent"), filepath.Join(dir, "dst")
				}},
				Expect: Expect{"no such file"},
			},
			{
				Name: "destination under a file",
				Input: Input{func(t *testing.T, dir string) (string, string) {
					return file(t, filepath.Join(dir, "src"), "x"), filepath.Join(file(t, filepath.Join(dir, "blocker"), ""), "dst")
				}},
				Expect: Expect{"not a directory"},
			},
			{
				Name: "destination is a directory",
				Input: Input{func(t *testing.T, dir string) (string, string) {
					dst := filepath.Join(dir, "dst")
					os.MkdirAll(filepath.Join(dst, "inside"), 0o755)
					return file(t, filepath.Join(dir, "src"), "x"), dst
				}},
				Expect: Expect{"file exists"},
			},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				src, dst := test.Input.Setup(t, t.TempDir())
				if err := copyFile(src, dst, 0o555); err == nil || !strings.Contains(err.Error(), test.Expect.ErrContains) {
					t.Fatalf("got %v, want %q", err, test.Expect.ErrContains)
				}
			})
		}
	})
}

func TestCommand(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input struct {
				Name string
				Args []string
			}
			Expect struct{}
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "true", Input: Input{Name: "true"}},
			{Name: "with arguments", Input: Input{"sh", []string{"-c", "exit 0"}}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				if err := command(test.Input.Name, test.Input.Args...); err != nil {
					t.Fatal(err)
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input struct {
				Name string
				Args []string
			}
			Expect struct{ ErrContains string }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "exit status with output", Input: Input{"sh", []string{"-c", "echo boom >&2; exit 3"}}, Expect: Expect{"sh -c echo boom >&2; exit 3: exit status 3: boom"}},
			{Name: "missing program", Input: Input{"no-such-program-xmr", nil}, Expect: Expect{"executable file not found"}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				if err := command(test.Input.Name, test.Input.Args...); err == nil || !strings.Contains(err.Error(), test.Expect.ErrContains) {
					t.Fatalf("got %v, want %q", err, test.Expect.ErrContains)
				}
			})
		}
	})
}
