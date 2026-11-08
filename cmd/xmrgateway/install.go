package main

import (
	"errors"
	"fmt"
	"os"
	"runtime"

	"github.com/RogueTeam/xmrgateway/internal/install"
)

func installCmd(args []string) error {
	o := install.Defaults()
	fs, home := newFlags("install")
	fs.StringVar(&o.Binary, "bin", o.Binary, "where to install the binary")
	fs.StringVar(&o.User, "user", o.User, "user the daemon runs as")
	fs.IntVar(&o.UID, "uid", o.UID, "uid for a new user")
	fs.StringVar(&o.Home, "home", o.Home, "home directory, holds config.json and static/")
	fs.StringVar(&o.Datasize, "datasize", o.Datasize, "datasize limit of the login class")
	fs.IntVar(&o.OpenFiles, "openfiles", o.OpenFiles, "open files limit of the login class")
	dryRun := fs.Bool("dry-run", false, "print the steps without running them")
	if err := parse(fs, home, args); err != nil {
		return err
	}

	self, err := os.Executable()
	if err != nil {
		return err
	}
	steps := install.Steps(o, self)
	if *dryRun {
		for i, s := range steps {
			fmt.Printf("%d. %s\n", i+1, s.Description)
		}
		return nil
	}
	if runtime.GOOS != "openbsd" {
		return errors.New("install only supports OpenBSD, use -dry-run to see the steps")
	}
	if os.Geteuid() != 0 {
		return errors.New("install must run as root")
	}
	for _, s := range steps {
		fmt.Println(s.Description)
		if err := s.Run(); err != nil {
			return err
		}
	}
	fmt.Printf("\ndone, review %s/config.json, then:\n  rcctl enable %s\n  rcctl start %s\n", o.Home, install.Name, install.Name)
	return nil
}
