// Package web embeds the HTML templates and the static assets.
package web

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed templates static
var files embed.FS

func Templates() fs.FS {
	sub, _ := fs.Sub(files, "templates")
	return sub
}

func Static() fs.FS {
	sub, _ := fs.Sub(files, "static")
	return sub
}

// Deploy writes the static assets to dir so a file server can serve them
// straight from disk.
func Deploy(dir string) error {
	return fs.WalkDir(Static(), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(dir, filepath.FromSlash(path))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := fs.ReadFile(Static(), path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}
