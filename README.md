# xmrgateway

Monero price API for everyone, served on the clearnet and on an onion address.

Price APIs live on the clearnet and many block Tor, so a hidden service that
shows Monero prices normally needs a clearnet jump box to fetch them for it.
xmrgateway does the polling and answers over Tor, so the hidden service gets
prices with nothing implemented or deployed outside Tor.

Clients pay for an API token with a hashcash proof of work instead of an
account. A token is good for one price request per minute. Everything runs
from one Go binary with no dependencies outside the standard library.

## Quick start

```sh
go build -o xmrgateway ./cmd/xmrgateway
cp config.example.json config.json   # set "secret" to 32+ random characters
./xmrgateway check -config config.json
./xmrgateway deploy                   # writes ./static
./xmrgateway server -config config.json
```

Getting a token: the "Generate token" button on the home page creates a
challenge and shows the commands for it. Solving one takes `pow`:

```sh
go run github.com/RogueTeam/xmrgateway/cmd/xmrgateway@latest pow PAYLOAD DIFFICULTY
```

`PAYLOAD` is the challenge id. The stamp it prints is posted to
`/api/v1/tokens`, and the page also shows both steps in one command:

```sh
curl -X POST https://xmrgateway.com/api/v1/tokens \
  -H 'Content-Type: application/json' \
  -d "{\"stamp\": \"$(go run github.com/RogueTeam/xmrgateway/cmd/xmrgateway@latest pow -until NOT_BEFORE ID 24)\"}"
```

Over Tor, prefix curl with `http_proxy=socks5h://127.0.0.1:9050` and use the
onion address; `socks5h` lets Tor resolve the `.onion` name.

`xmrgateway token -url URL` does the whole exchange in one go and prints the
token.

## Commands

The first argument picks the command, the rest are its flags. Every command
accepts `-chdir-home` to change to the user's home directory before doing
anything else.

| Command   | Does |
|-----------|------|
| `server`  | Serves the API, the website and the static files. `-log-time=false` leaves the time to syslog. |
| `daemon`  | Runs `server` as a child and restarts it when it exits. The rc.d script runs this. |
| `install` | Installs the binary as an OpenBSD rc.d service. |
| `deploy`  | Writes the embedded static assets to `-dir` (default `static`). |
| `pow`     | `pow PAYLOAD DIFFICULTY` solves a challenge on every core and prints the stamp. `-until` waits for `not_before` first. |
| `token`   | Requests or reuses a challenge, solves it, waits and prints a token. |
| `prices`  | Polls every price source once and prints what each returned. |
| `check`   | Validates a config file. |
| `schema`  | Prints the config JSON schema. |
| `version` | Prints the version. |

## Configuration

`config.example.json` lists every field. The schema is in
`internal/config/config.schema.json` (also `xmrgateway schema`), and durations
are Go duration strings such as `30s` or `24h`. The server refuses to start
when `Validate` finds a problem and reports all of them at once.

Only `secret` is required. After unmarshalling, `Default` fills every missing
or zero field, then `Validate` runs. The settings that shape the token flow:

- `secret` signs the tokens. Changing it invalidates every token.
- `token_validity` is how long a token lasts (default `2160h`, 90 days; Go
  durations have no day unit).
- `challenge_lifetime` is how long a challenge can be redeemed after it was
  issued (default `1h`).
- `challenge_min_wait` is the shortest time between issuing and redeeming a
  challenge (default `30s`). This, not the proof of work, guarantees that
  getting a token takes at least that long on any hardware.
- `pow_difficulty` is the number of leading zero bits the stamp needs
  (default `24`, a few seconds on one core).
- `rate_limit` is the time between two price requests per token (default `30s`).
- `price_refresh` is how often the price worker polls the sources (default `10m`).
- `request_timeout` bounds every HTTP request (default `30s`).

Requests whose host is in `clearnet_hosts` get an `Onion-Location` header
pointing at `onion_url`.

## API

| Method | Path | |
|--------|------|-|
| `POST` | `/api/v1/challenges` | New challenge: id, difficulty, `not_before`, `expires_at`. |
| `GET`  | `/api/v1/challenges/{id}` | Look up a pending challenge. |
| `POST` | `/api/v1/tokens` | Redeem `{"stamp": "..."}` for a token. |
| `GET`  | `/api/v1/price?currency=USD` | Price of 1 XMR. Needs `X-API-Token`. |

A price response looks like this:

```json
{
  "currency": "USD",
  "price": "312.45",
  "updated_at": "2026-11-07T12:00:00Z",
  "next_request_at": "2026-11-07T12:01:31Z"
}
```

Asking again before `next_request_at`, or without a token, returns `429` with
`Retry-After` and `retry_at`. An invalid or expired token returns `401`. The
full reference is served at `/docs`, built from `httpmux/docs/swagger.json`.

Stamps are hashcash version 1 over SHA-224, with the challenge id as the
resource. See `hashcash/` and the `/docs` page for the exact format.

## Prices

`sources/` has one client per source used by the Haveno price node: Kraken,
Bitfinex, KuCoin, Poloniex, CoinGecko, Binance, Bitstamp, Coinbase, bitFlyer,
BTC Markets, Coinone, Independent Reserve, Luno, Mercado Bitcoin, Paribu,
CriptoYa and Yadio. BlueLytics is used for the Argentine street-rate gap.

A background worker polls the sources every `price_refresh`, builds the
consensus and swaps it in with one atomic pointer store. Requests only read
that cache and never wait on an upstream API.

Prices never go through floats. Each quote is parsed from its decimal text
into a `uint64` with two decimals for fiat and eight for BTC. For every
currency, direct XMR markets and BTC markets (through the XMR/BTC consensus)
all vote. Votes further than `max_deviation_bps` from the median are rejected
and logged, and the rest are averaged.

`xmrgateway prices` shows what every source answered. Run it once after
deploying: the clients were written against the documented response formats
and have not all been checked against the live APIs.

## OpenBSD

As root, on the machine that will run it:

```sh
./xmrgateway install -dry-run   # list the steps
./xmrgateway install
rcctl enable xmrgateway
rcctl start xmrgateway
```

`install` copies the binary to `/usr/local/bin/xmrgateway`, creates the
`_xmrgateway` user (uid 5000) with its home, writes a `config.json` with a
random secret and the static files there, and installs `/etc/rc.d/xmrgateway`,
which runs `xmrgateway daemon -chdir-home -config config.json`.

Memory and file limits come from the login class in
`/etc/login.conf.d/xmrgateway` (`-datasize`, `-openfiles`). rc.subr runs a
daemon with the login class named like its rc.d script, so nothing else
needs to point at it. Logs go to syslog as `daemon.info`.

rc.d does not restart a service that dies, so `daemon` does: it checks the
config, then keeps one `server -log-time=false` child running and starts a
new one when it exits, at most once per `-restart-interval` (default 10s,
2s minimum). A small worker process sits between them and stops the server
if `daemon` itself is killed, so `rcctl stop` and `kill -9` never leave a
server behind. `daemon` only pledges `stdio rpath proc exec`; the server
starts unpledged after exec and locks itself down as below.

After reading the config, `server` unveils only the static directory,
`/etc/resolv.conf` and `/etc/hosts` (read only), then pledges
`stdio rpath inet dns`. Pledge and unveil are called through libc without
`golang.org/x/sys`, on amd64, arm64 and riscv64. On other systems these
calls do nothing.

## Development

```sh
go test ./...
npm ci              # standard-version, for releases
npm run docs        # regenerate httpmux/docs with swag
```

Pushing to `dev` runs standard-version, which bumps the version in
`package.json`, the OpenAPI annotations and the generated docs, updates
`CHANGELOG.md` and tags the commit. Pushing to `main` tests, creates the
release and uploads binaries for OpenBSD, Linux and FreeBSD on amd64 and
arm64, plus a container image on ghcr.io.

The logo in `web/static/logo.svg` is a placeholder. Replace it with the
Monero symbol from the official press kit.
