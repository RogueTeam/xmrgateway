// Package install sets xmrgateway up as an OpenBSD rc.d service.
package install

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/RogueTeam/xmrgateway/internal/config"
	"github.com/RogueTeam/xmrgateway/web"
)

// Name is used for the rc.d script and the login class. rc.subr picks the
// login class with the same name as the script, so no daemon_class is set.
const Name = "xmrgateway"

type Options struct {
	Binary    string
	User      string
	UID       int
	Home      string
	Datasize  string
	OpenFiles int
}

func Defaults() Options {
	return Options{
		Binary:    "/usr/local/bin/xmrgateway",
		User:      "_xmrgateway",
		UID:       5000,
		Home:      "/home/_xmrgateway",
		Datasize:  "4096M",
		OpenFiles: 4096,
	}
}

type Step struct {
	Description string
	Run         func() error
}

func RCScript(o Options) string {
	return fmt.Sprintf(`#!/bin/ksh

daemon=%q
daemon_flags="daemon -chdir-home -config config.json"
daemon_user=%q
daemon_logger="daemon.info"

. /etc/rc.d/rc.subr

rc_bg=YES
rc_reload=NO

rc_cmd $1
`, o.Binary, o.User)
}

func LoginClass(o Options) string {
	return fmt.Sprintf("%s:\\\n\t:datasize=%s:\\\n\t:openfiles=%d:\\\n\t:tc=daemon:\n", Name, o.Datasize, o.OpenFiles)
}

// NewConfig returns the defaults with a fresh random secret.
func NewConfig() (config.Config, error) {
	var cfg config.Config
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return cfg, err
	}
	cfg.Secret = hex.EncodeToString(secret)
	cfg.Default()
	return cfg, cfg.Validate()
}

// Steps lists the changes to make, in order. self is the running binary.
func Steps(o Options, self string) []Step {
	rcPath := filepath.Join("/etc/rc.d", Name)
	classPath := filepath.Join("/etc/login.conf.d", Name)
	configPath := filepath.Join(o.Home, "config.json")
	staticPath := filepath.Join(o.Home, "static")

	return []Step{
		{"copy " + self + " to " + o.Binary, func() error { return copyFile(self, o.Binary, 0o555) }},
		{"write login class " + classPath, func() error {
			if err := os.MkdirAll(filepath.Dir(classPath), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(classPath, []byte(LoginClass(o)), 0o644); err != nil {
				return err
			}
			if _, err := os.Stat("/etc/login.conf.db"); err == nil {
				return command("cap_mkdb", "/etc/login.conf")
			}
			return nil
		}},
		{fmt.Sprintf("create user %s with uid %d, class %s and home %s", o.User, o.UID, Name, o.Home), func() error {
			if _, err := user.Lookup(o.User); err == nil {
				return command("usermod", "-L", Name, o.User)
			}
			return command("useradd", "-u", strconv.Itoa(o.UID), "-g", "=uid", "-L", Name,
				"-c", "xmrgateway daemon", "-d", o.Home, "-s", "/sbin/nologin", "-m", o.User)
		}},
		{"write " + configPath + " with a random secret, unless it exists", func() error {
			if _, err := os.Stat(configPath); err == nil {
				return nil
			}
			cfg, err := NewConfig()
			if err != nil {
				return err
			}
			data, err := json.MarshalIndent(cfg, "", "  ")
			if err != nil {
				return err
			}
			if err := os.WriteFile(configPath, append(data, '\n'), 0o600); err != nil {
				return err
			}
			return chown(o.User, configPath)
		}},
		{"deploy static assets to " + staticPath, func() error {
			if err := web.Deploy(staticPath); err != nil {
				return err
			}
			return chown(o.User, staticPath)
		}},
		{"write " + rcPath, func() error { return os.WriteFile(rcPath, []byte(RCScript(o)), 0o555) }},
	}
}

func command(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func chown(username, root string) error {
	u, err := user.Lookup(username)
	if err != nil {
		return err
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	return filepath.Walk(root, func(path string, _ os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		return os.Lchown(path, uid, gid)
	})
}

func copyFile(src, dst string, mode os.FileMode) error {
	if same, _ := filepath.EvalSymlinks(src); same == dst {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".new"
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}
