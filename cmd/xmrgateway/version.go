package main

import (
	"fmt"
	"runtime/debug"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func currentVersion() string {
	if version != "dev" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return version
}

func versionCmd(args []string) error {
	fs, home := newFlags("version")
	if err := parse(fs, home, args); err != nil {
		return err
	}
	fmt.Println(currentVersion())
	return nil
}
