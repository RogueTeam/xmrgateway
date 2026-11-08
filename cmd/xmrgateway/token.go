package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/RogueTeam/xmrgateway/client"
)

func tokenCmd(args []string) error {
	fs, home := newFlags("token")
	base := fs.String("url", "https://xmrgateway.com", "gateway URL")
	challenge := fs.String("challenge", "", "challenge id from the website, a new one is requested when empty")
	proxy := fs.String("proxy", "", "proxy URL, e.g. socks5h://127.0.0.1:9050 for Tor")
	timeout := fs.Duration("timeout", 10*time.Minute, "give up after this long")
	if err := parse(fs, home, args); err != nil {
		return err
	}
	httpc, err := httpClient(*proxy, time.Minute)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	fmt.Fprintln(os.Stderr, "solving the challenge and waiting out the minimum delay...")
	c := &client.Client{BaseURL: *base, HTTP: httpc}
	token, err := c.Token(ctx, *challenge)
	if err != nil {
		return err
	}
	fmt.Println(token.Token)
	fmt.Fprintln(os.Stderr, "valid until", token.ExpiresAt.UTC().Format(time.RFC3339))
	return nil
}
