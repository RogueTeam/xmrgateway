package httpmux

import (
	"fmt"
	"strings"
	"time"

	"github.com/RogueTeam/xmrgateway/api"
	"github.com/RogueTeam/xmrgateway/controller"
)

const goRun = "go run github.com/RogueTeam/xmrgateway/cmd/xmrgateway@latest"

// network holds the curl commands for one way of reaching the gateway.
type network struct {
	Name    string
	Redeem  string
	OneShot string
	Price   string
}

type tokenCommands struct {
	Pow      string
	Networks []network
}

func (m *Mux) tokenCommands(ch controller.Challenge) tokenCommands {
	pow := fmt.Sprintf("%s \\\n  pow %s %d", goRun, ch.ID, ch.Difficulty)
	waitAndPow := fmt.Sprintf("%s \\\n    pow -until %s %s %d", goRun, ch.NotBefore.UTC().Format(time.RFC3339), ch.ID, ch.Difficulty)

	cmds := tokenCommands{Pow: pow}
	add := func(name, base, proxy string) {
		base = strings.TrimRight(base, "/")
		redeem := func(body string) string {
			return fmt.Sprintf("%scurl -X POST \\\n  %s/api/v1/tokens \\\n  -H 'Content-Type: application/json' \\\n  -d %s", proxy, base, body)
		}
		cmds.Networks = append(cmds.Networks, network{
			Name:    name,
			Redeem:  redeem(`'{"stamp": "STAMP"}'`),
			OneShot: redeem(`"{\"stamp\": \"$(` + waitAndPow + `)\"}"`),
			Price:   fmt.Sprintf("%scurl -H '%s: TOKEN' \\\n  '%s/api/v1/price?currency=USD'", proxy, api.TokenHeader, base),
		})
	}
	add("Clearnet", m.cfg.PublicURL, "")
	if m.cfg.OnionURL != "" {
		add("Tor", m.cfg.OnionURL, torProxy(m.cfg.OnionURL))
	}
	return cmds
}

// torProxy sends curl through the local Tor daemon. With socks5h Tor also
// resolves the onion address.
func torProxy(url string) string {
	if strings.HasPrefix(url, "https://") {
		return "https_proxy=socks5h://127.0.0.1:9050 \\\n"
	}
	return "http_proxy=socks5h://127.0.0.1:9050 \\\n"
}
