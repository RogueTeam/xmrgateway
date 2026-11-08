package main

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/RogueTeam/xmrgateway/sources"
)

func pricesCmd(args []string) error {
	fs, home := newFlags("prices")
	proxy := fs.String("proxy", "", "proxy URL, e.g. socks5h://127.0.0.1:9050 for Tor")
	timeout := fs.Duration("timeout", 20*time.Second, "per-source timeout")
	deviation := fs.Uint64("max-deviation-bps", 500, "reject quotes further than this from the median")
	asJSON := fs.Bool("json", false, "print the consensus prices as JSON")
	if err := parse(fs, home, args); err != nil {
		return err
	}
	httpc, err := httpClient(*proxy, *timeout)
	if err != nil {
		return err
	}
	results := sources.FetchAll(context.Background(), *timeout, sources.All(httpc)...)
	prices := sources.Aggregate(sources.AllRates(results), sources.Options{MaxDeviationBps: *deviation})

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(prices)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SOURCE\tRATES\tTOOK\tSTATUS")
	for _, r := range results {
		status := "ok"
		if r.Err != nil {
			status = strings.ReplaceAll(r.Err.Error(), "\n", "; ")
		}
		fmt.Fprintf(w, "%s\t%d\t%v\t%.100s\n", r.Source, len(r.Rates), r.Took.Round(time.Millisecond), status)
	}
	fmt.Fprintln(w, "\n1 XMR\tPRICE\tSOURCES\tREJECTED")
	for _, quote := range slices.Sorted(maps.Keys(prices)) {
		p := prices[quote]
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\n", quote, p, len(p.Sources), strings.Join(p.Rejected, " "))
	}
	return w.Flush()
}
