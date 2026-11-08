package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"time"

	"github.com/RogueTeam/xmrgateway/client"
)

func powCmd(args []string) error {
	fs, home := newFlags("pow")
	until := fs.String("until", "", "wait until this RFC 3339 time, the challenge's not_before, before printing")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "usage: xmrgateway pow [flags] PAYLOAD DIFFICULTY")
		fs.PrintDefaults()
	}
	if err := parse(fs, home, args); err != nil {
		return err
	}
	if fs.NArg() != 2 {
		fs.Usage()
		return errors.New("PAYLOAD and DIFFICULTY are required")
	}
	difficulty, err := strconv.Atoi(fs.Arg(1))
	if err != nil || difficulty < 1 || difficulty > 64 {
		return fmt.Errorf("DIFFICULTY must be a number between 1 and 64, got %q", fs.Arg(1))
	}
	var notBefore time.Time
	if *until != "" {
		if notBefore, err = time.Parse(time.RFC3339, *until); err != nil {
			return fmt.Errorf("-until: %w", err)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	stamp, err := client.Solve(ctx, fs.Arg(0), difficulty)
	if err != nil {
		return err
	}
	if wait := time.Until(notBefore); wait > 0 {
		// A second of slack for clocks that run behind the server's.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait + time.Second):
		}
	}
	fmt.Println(stamp)
	return nil
}
