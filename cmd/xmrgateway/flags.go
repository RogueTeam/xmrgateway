package main

import (
	"flag"
	"net/http"
	"net/url"
	"os"
	"time"
)

// newFlags returns a flag set with the -chdir-home flag every command shares.
func newFlags(name string) (*flag.FlagSet, *bool) {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	home := fs.Bool("chdir-home", false, "change to the user's home directory first")
	return fs, home
}

func parse(fs *flag.FlagSet, chdirHome *bool, args []string) error {
	fs.Parse(args)
	if !*chdirHome {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	return os.Chdir(home)
}

func httpClient(proxy string, timeout time.Duration) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if proxy != "" {
		u, err := url.Parse(proxy)
		if err != nil {
			return nil, err
		}
		transport.Proxy = http.ProxyURL(u)
	}
	return &http.Client{Transport: transport, Timeout: timeout}, nil
}
