package main

import (
	"fmt"
	"os"
	"sort"
)

type command struct {
	run     func(args []string) error
	summary string
}

var commands = map[string]command{
	"daemon":  {daemonCmd, "run server and restart it when it exits; used by the rc.d script"},
	"install": {installCmd, "install as an OpenBSD rc.d service"},
	"server":  {serverCmd, "serve the API and the website"},
	"deploy":  {deployCmd, "write the static assets to disk"},
	"token":   {tokenCmd, "get an API token from a gateway"},
	"pow":     {powCmd, "solve a challenge and print the stamp: pow PAYLOAD DIFFICULTY"},
	"prices":  {pricesCmd, "poll every price source once and print the result"},
	"check":   {checkCmd, "validate a config file"},
	"schema":  {schemaCmd, "print the config JSON schema"},
	"version": {versionCmd, "print the version"},
}

func usage() {
	fmt.Fprintf(os.Stderr, "usage: %s <command> [flags]\n\ncommands:\n", os.Args[0])
	names := make([]string, 0, len(commands))
	for name := range commands {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(os.Stderr, "  %-8s %s\n", name, commands[name].summary)
	}
	fmt.Fprintf(os.Stderr, "\nrun %s <command> -h for its flags\n", os.Args[0])
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd, ok := commands[os.Args[1]]
	if !ok {
		usage()
		os.Exit(2)
	}
	if err := cmd.run(os.Args[2:]); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", os.Args[1], err)
		os.Exit(1)
	}
}
