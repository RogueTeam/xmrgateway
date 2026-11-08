package web

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestEmbedded(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input  struct{ FS func() fs.FS }
			Expect struct{ Files []string }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "templates", Input: Input{Templates}, Expect: Expect{[]string{"docs.html", "home.html", "layout.html", "token.html"}}},
			{Name: "static", Input: Input{Static}, Expect: Expect{[]string{"logo.svg", "pico.orange.min.css"}}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				var got []string
				fs.WalkDir(test.Input.FS(), ".", func(path string, d fs.DirEntry, err error) error {
					if err == nil && !d.IsDir() {
						got = append(got, path)
					}
					return err
				})
				if !slices.Equal(got, test.Expect.Files) {
					t.Fatalf("got %v", got)
				}
			})
		}
	})
}

func TestNoCustomCSS(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input  struct{ Template string }
			Expect struct{ Absent []string }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		absent := []string{"<style", "style=", "<script", "javascript:"}
		tests := []Test{
			{Name: "layout", Input: Input{"layout.html"}, Expect: Expect{absent}},
			{Name: "home", Input: Input{"home.html"}, Expect: Expect{absent}},
			{Name: "token", Input: Input{"token.html"}, Expect: Expect{absent}},
			{Name: "docs", Input: Input{"docs.html"}, Expect: Expect{absent}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				b, err := fs.ReadFile(Templates(), test.Input.Template)
				if err != nil {
					t.Fatal(err)
				}
				for _, a := range test.Expect.Absent {
					if strings.Contains(string(b), a) {
						t.Errorf("%s has %q", test.Input.Template, a)
					}
				}
			})
		}
	})
}

func TestDeploy(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input struct {
				Setup func(t *testing.T, dir string) string
			}
			Expect struct{ Mode fs.FileMode }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "new directory", Input: Input{func(t *testing.T, dir string) string { return filepath.Join(dir, "static") }}, Expect: Expect{0o644}},
			{Name: "nested new directory", Input: Input{func(t *testing.T, dir string) string { return filepath.Join(dir, "a", "b", "static") }}, Expect: Expect{0o644}},
			{Name: "existing empty directory", Input: Input{func(t *testing.T, dir string) string { return dir }}, Expect: Expect{0o644}},
			{
				Name: "stale files are overwritten",
				Input: Input{func(t *testing.T, dir string) string {
					os.WriteFile(filepath.Join(dir, "logo.svg"), []byte("old"), 0o644)
					return dir
				}},
				Expect: Expect{0o644},
			},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				target := test.Input.Setup(t, t.TempDir())
				if err := Deploy(target); err != nil {
					t.Fatal(err)
				}
				fs.WalkDir(Static(), ".", func(path string, d fs.DirEntry, err error) error {
					if err != nil || d.IsDir() {
						return err
					}
					want, _ := fs.ReadFile(Static(), path)
					got, err := os.ReadFile(filepath.Join(target, path))
					if err != nil || !bytes.Equal(got, want) {
						t.Errorf("%s differs from the embedded copy: %v", path, err)
					}
					if info, _ := os.Stat(filepath.Join(target, path)); info.Mode().Perm() != test.Expect.Mode {
						t.Errorf("%s mode %v", path, info.Mode())
					}
					return nil
				})
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input struct {
				Setup func(t *testing.T, dir string) string
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
				Name: "target is a file",
				Input: Input{func(t *testing.T, dir string) string {
					p := filepath.Join(dir, "static")
					os.WriteFile(p, nil, 0o644)
					return p
				}},
				Expect: Expect{"not a directory"},
			},
			{
				Name: "target under a file",
				Input: Input{func(t *testing.T, dir string) string {
					p := filepath.Join(dir, "file")
					os.WriteFile(p, nil, 0o644)
					return filepath.Join(p, "static")
				}},
				Expect: Expect{"not a directory"},
			},
			{
				Name: "an asset path is taken by a directory",
				Input: Input{func(t *testing.T, dir string) string {
					os.MkdirAll(filepath.Join(dir, "logo.svg", "x"), 0o755)
					return dir
				}},
				Expect: Expect{"is a directory"},
			},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				if err := Deploy(test.Input.Setup(t, t.TempDir())); err == nil || !strings.Contains(err.Error(), test.Expect.ErrContains) {
					t.Fatalf("got %v, want %q", err, test.Expect.ErrContains)
				}
			})
		}
	})
}
