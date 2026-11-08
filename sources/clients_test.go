package sources

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestClients(t *testing.T) {
	now := time.Now()
	sec, ms := now.Unix(), now.UnixMilli()
	iso := now.UTC().Format(time.RFC3339Nano)

	t.Run("Succeed", func(t *testing.T) {
		type (
			Input struct {
				Routes map[string]reply
				Source func(baseURL string) Source
			}
			Expect struct {
				Name  string
				Pairs []string
			}
			Test struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{
				Name: "kraken",
				Input: Input{
					Routes: map[string]reply{"/0/public/Ticker": ok(`{"error":[],"result":{
						"XXMRZUSD":{"a":["301.00","1","1.000"],"b":["299.00","2","2.000"],"c":["300.50","0.1"]},
						"XXMRXXBT":{"a":["0.00310","1","1"],"b":["0.00300","1","1"],"c":["0.00305","1"]},
						"XMREUR":{"a":["281.0","1","1"],"b":["279.0","1","1"],"c":["280.0","1"]},
						"XXBTZEUR":{"a":["90010.0","1","1"],"b":["89990.0","1","1"],"c":["90000.0","1"]},
						"XBTCHF":{"a":["80010","1","1"],"b":["79990","1","1"],"c":["80000","1"]},
						"XMRUSDT":{"a":["300","1","1"],"b":["300","1","1"],"c":["300","1"]},
						"XETHZUSD":{"a":["3000","1","1"],"b":["3000","1","1"],"c":["3000","1"]},
						"XXBTZGBP":{"a":[],"b":[],"c":[]}}}`)},
					Source: func(u string) Source { return Kraken{BaseURL: u} },
				},
				Expect: Expect{"kraken", []string{"BTC/CHF=80000.00", "BTC/EUR=90000.00", "XMR/BTC=0.00305000", "XMR/EUR=280.00", "XMR/USD=300.00"}},
			},
			{
				Name: "bitfinex",
				Input: Input{
					Routes: map[string]reply{"/v2/tickers?symbols=ALL": ok(`[
						["tXMRUSD",299,10,301,10,1,0.01,300.5,1000,310,290],
						["tXMRBTC",0.0030,10,0.0031,10,0,0,0.00305,100,0.0032,0.0029],
						["tBTCEUR",89990,1,90010,1,0,0,90000,10,91000,89000],
						["tBTCUST",95000,1,95010,1,0,0,95005,10,96000,94000],
						["tTESTBTC:TESTUSD",1,1,1,1,0,0,1,1,1,1],
						["tXMRJPY",null,null,null,null,0,0,45000,1,1,1],
						["tSHORT",1,2],
						["fUSD",0.0001,0.0002,2,100,0.0001,30,100,0,0,0.0002,1000,0,0,null,null,1000]]`)},
					Source: func(u string) Source { return Bitfinex{BaseURL: u} },
				},
				Expect: Expect{"bitfinex", []string{"BTC/EUR=90000.00", "XMR/BTC=0.00305000", "XMR/JPY=45000.00", "XMR/USD=300.00"}},
			},
			{
				Name: "kucoin",
				Input: Input{
					Routes: map[string]reply{"/api/v1/market/allTickers": ok(fmt.Sprintf(`{"code":"200000","data":{"time":%d,"ticker":[
						{"symbol":"XMR-BTC","buy":"0.0030","sell":"0.0031","last":"0.00305"},
						{"symbol":"XMR-USDT","buy":"299","sell":"301","last":"300"},
						{"symbol":"BTC-EUR","buy":"89990","sell":"90010","last":"90000"},
						{"symbol":"NOSEPARATOR","buy":"1","sell":"1","last":"1"}]}}`, ms))},
					Source: func(u string) Source { return KuCoin{BaseURL: u} },
				},
				Expect: Expect{"kucoin", []string{"BTC/EUR=90000.00", "XMR/BTC=0.00305000"}},
			},
			{
				Name: "poloniex",
				Input: Input{
					Routes: map[string]reply{"/markets/price": ok(fmt.Sprintf(`[
						{"symbol":"XMR_BTC","price":"0.00305","ts":%[1]d},
						{"symbol":"XMR_USDT","price":"300","ts":%[1]d},
						{"symbol":"ETH_BTC","price":"0.03","ts":%[1]d}]`, ms))},
					Source: func(u string) Source { return Poloniex{BaseURL: u} },
				},
				Expect: Expect{"poloniex", []string{"XMR/BTC=0.00305000"}},
			},
			{
				Name: "coingecko",
				Input: Input{
					Routes: map[string]reply{"/api/v3/exchange_rates": ok(`{"rates":{
						"btc":{"value":1.0,"type":"crypto"},
						"usd":{"value":100000.0,"type":"fiat"},
						"eur":{"value":90000.0,"type":"fiat"},
						"xmr":{"value":327.86885,"type":"crypto"},
						"xau":{"value":30.0,"type":"commodity"}}}`)},
					Source: func(u string) Source { return CoinGecko{BaseURL: u} },
				},
				Expect: Expect{"coingecko", []string{"BTC/EUR=90000.00", "BTC/USD=100000.00", "XMR/BTC=0.00305000"}},
			},
			{
				Name: "binance",
				Input: Input{
					Routes: map[string]reply{"/api/v3/ticker/bookTicker": ok(`[
						{"symbol":"BTCEUR","bidPrice":"89990.00","askPrice":"90010.00"},
						{"symbol":"BTCTRY","bidPrice":"3999000","askPrice":"4001000"},
						{"symbol":"BTCUSDT","bidPrice":"95000","askPrice":"95010"},
						{"symbol":"XMRBTC","bidPrice":"0.003","askPrice":"0.003"}]`)},
					Source: func(u string) Source { return Binance{BaseURL: u} },
				},
				Expect: Expect{"binance", []string{"BTC/EUR=90000.00", "BTC/TRY=4000000.00"}},
			},
			{
				Name: "bitstamp",
				Input: Input{
					Routes: map[string]reply{"/api/v2/ticker/": ok(fmt.Sprintf(`[
						{"timestamp":"%[1]d","last":"100000","bid":"99990","ask":"100010","pair":"BTC/USD"},
						{"timestamp":"%[1]d","last":"90000","bid":"89990","ask":"90010","pair":"BTC/EUR"},
						{"timestamp":"%[1]d","last":"3000","bid":"2999","ask":"3001","pair":"ETH/USD"},
						{"timestamp":"%[1]d","last":"1","bid":"1","ask":"1","pair":"BTCUSD"}]`, sec))},
					Source: func(u string) Source { return Bitstamp{BaseURL: u} },
				},
				Expect: Expect{"bitstamp", []string{"BTC/EUR=90000.00", "BTC/USD=100000.00"}},
			},
			{
				Name: "coinbase",
				Input: Input{
					Routes: map[string]reply{
						"/products/BTC-USD/ticker": ok(fmt.Sprintf(`{"ask":"100010","bid":"99990","price":"100000","time":%q}`, iso)),
						"/products/BTC-EUR/ticker": ok(fmt.Sprintf(`{"ask":"90010","bid":"89990","price":"90000","time":%q}`, iso)),
						"/products/BTC-GBP/ticker": ok(fmt.Sprintf(`{"ask":"80010","bid":"79990","price":"80000","time":%q}`, iso)),
					},
					Source: func(u string) Source { return Coinbase{BaseURL: u} },
				},
				Expect: Expect{"coinbase", []string{"BTC/EUR=90000.00", "BTC/GBP=80000.00", "BTC/USD=100000.00"}},
			},
			{
				Name: "coinbase with one product down",
				Input: Input{
					Routes: map[string]reply{"/products/BTC-USD/ticker": ok(`{"price":"100000"}`), "/products/BTC-EUR/ticker": {500, "down"}},
					Source: func(u string) Source { return Coinbase{BaseURL: u, Products: []string{"BTC-USD", "BTC-EUR"}} },
				},
				Expect: Expect{"coinbase", []string{"BTC/USD=100000.00"}},
			},
			{
				Name: "bitflyer",
				Input: Input{
					Routes: map[string]reply{"/v1/getticker?product_code=BTC_JPY": ok(fmt.Sprintf(
						`{"product_code":"BTC_JPY","timestamp":%q,"best_bid":14999000,"best_ask":15001000,"ltp":15000500}`,
						now.UTC().Format("2006-01-02T15:04:05.99")))},
					Source: func(u string) Source { return Bitflyer{BaseURL: u} },
				},
				Expect: Expect{"bitflyer", []string{"BTC/JPY=15000000.00"}},
			},
			{
				Name: "btcmarkets",
				Input: Input{
					Routes: map[string]reply{"/v3/markets/tickers?marketId=BTC-AUD": ok(fmt.Sprintf(
						`[{"marketId":"BTC-AUD","bestBid":"149990","bestAsk":"150010","lastPrice":"150000","timestamp":%q}]`, iso))},
					Source: func(u string) Source { return BTCMarkets{BaseURL: u} },
				},
				Expect: Expect{"btcmarkets", []string{"BTC/AUD=150000.00"}},
			},
			{
				Name: "coinone",
				Input: Input{
					Routes: map[string]reply{"/public/v2/ticker_new/KRW/BTC": ok(fmt.Sprintf(`{"result":"success","error_code":"0",
						"tickers":[{"quote_currency":"krw","target_currency":"btc","timestamp":%d,"last":"140000000",
						"best_asks":[{"price":"140010000","qty":"1"}],"best_bids":[{"price":"139990000","qty":"1"}]}]}`, ms))},
					Source: func(u string) Source { return Coinone{BaseURL: u} },
				},
				Expect: Expect{"coinone", []string{"BTC/KRW=140000000.00"}},
			},
			{
				Name: "coinone without order book",
				Input: Input{
					Routes: map[string]reply{"/public/v2/ticker_new/KRW/BTC": ok(`{"result":"success","error_code":"0",
						"tickers":[{"quote_currency":"KRW","target_currency":"BTC","last":"140000000","best_asks":[],"best_bids":[]}]}`)},
					Source: func(u string) Source { return Coinone{BaseURL: u} },
				},
				Expect: Expect{"coinone", []string{"BTC/KRW=140000000.00"}},
			},
			{
				Name: "independentreserve",
				Input: Input{
					Routes: map[string]reply{
						"/Public/GetMarketSummary?primaryCurrencyCode=Xbt&secondaryCurrencyCode=Aud": ok(fmt.Sprintf(`{"CreatedTimestampUtc":%q,"CurrentHighestBidPrice":149990,"CurrentLowestOfferPrice":150010,"LastPrice":150000}`, iso)),
						"/Public/GetMarketSummary?primaryCurrencyCode=Xbt&secondaryCurrencyCode=Nzd": ok(fmt.Sprintf(`{"CreatedTimestampUtc":%q,"CurrentHighestBidPrice":164990,"CurrentLowestOfferPrice":165010,"LastPrice":165000}`, iso)),
						"/Public/GetMarketSummary?primaryCurrencyCode=Xbt&secondaryCurrencyCode=Usd": ok(fmt.Sprintf(`{"CreatedTimestampUtc":%q,"CurrentHighestBidPrice":99990,"CurrentLowestOfferPrice":100010,"LastPrice":100000}`, iso)),
					},
					Source: func(u string) Source { return IndependentReserve{BaseURL: u} },
				},
				Expect: Expect{"independentreserve", []string{"BTC/AUD=150000.00", "BTC/NZD=165000.00", "BTC/USD=100000.00"}},
			},
			{
				Name: "luno",
				Input: Input{
					Routes: map[string]reply{"/api/1/tickers": ok(fmt.Sprintf(`{"tickers":[
						{"pair":"XBTZAR","timestamp":%[1]d,"bid":"1799900","ask":"1800100","last_trade":"1800000","status":"ACTIVE"},
						{"pair":"XBTNGN","timestamp":%[1]d,"bid":"1","ask":"1","last_trade":"1","status":"DISABLED"},
						{"pair":"XBTMYR","timestamp":%[1]d,"bid":"0","ask":"0","last_trade":"450000","status":"POSTONLY"},
						{"pair":"ETHZAR","timestamp":%[1]d,"bid":"60000","ask":"60010","last_trade":"60005","status":"ACTIVE"}]}`, ms))},
					Source: func(u string) Source { return Luno{BaseURL: u} },
				},
				Expect: Expect{"luno", []string{"BTC/MYR=450000.00", "BTC/ZAR=1800000.00"}},
			},
			{
				Name: "mercadobitcoin",
				Input: Input{
					Routes: map[string]reply{"/api/v4/tickers?symbols=BTC-BRL": ok(fmt.Sprintf(
						`[{"pair":"BTC-BRL","last":"550000","buy":"549900","sell":"550100","date":%d}]`, sec))},
					Source: func(u string) Source { return MercadoBitcoin{BaseURL: u} },
				},
				Expect: Expect{"mercadobitcoin", []string{"BTC/BRL=550000.00"}},
			},
			{
				Name: "paribu",
				Input: Input{
					Routes: map[string]reply{"/ticker": ok(`{
						"BTC_TL":{"last":4000000,"lowestAsk":4001000,"highestBid":3999000},
						"ETH_TL":{"last":100000,"lowestAsk":100100,"highestBid":99900},
						"BROKEN":{"last":1}}`)},
					Source: func(u string) Source { return Paribu{BaseURL: u} },
				},
				Expect: Expect{"paribu", []string{"BTC/TRY=4000000.00"}},
			},
			{
				Name: "criptoya",
				Input: Input{
					Routes: map[string]reply{"/api/btc/ars/0.1": ok(fmt.Sprintf(`{
						"bitso":{"ask":130000000,"time":%[1]d},
						"ripio":{"ask":131000000,"time":%[1]d},
						"notlisted":{"ask":1,"time":%[1]d},
						"belo":{"ask":132000000,"time":%[2]d}}`, sec, sec-3*86400))},
					Source: func(u string) Source { return CriptoYa{BaseURL: u} },
				},
				Expect: Expect{"criptoya", []string{"BTC/ARS=130000000.00", "BTC/ARS=131000000.00"}},
			},
			{
				Name: "criptoya with a custom whitelist",
				Input: Input{
					Routes: map[string]reply{"/api/btc/ars/0.1": ok(fmt.Sprintf(`{"bitso":{"ask":130000000,"time":%[1]d},"mine":{"ask":129000000,"time":%[1]d}}`, sec))},
					Source: func(u string) Source { return CriptoYa{BaseURL: u, Exchanges: []string{"mine"}} },
				},
				Expect: Expect{"criptoya", []string{"BTC/ARS=129000000.00"}},
			},
			{
				Name: "yadio",
				Input: Input{
					Routes: map[string]reply{"/exrates": ok(fmt.Sprintf(`{"BTC":100000,"USD":{"ARS":1300,"BOB":13.5,"EUR":0.9,"LBP":89500},"base":"USD","timestamp":%d}`, ms))},
					Source: func(u string) Source { return Yadio{BaseURL: u} },
				},
				Expect: Expect{"yadio", []string{"BTC/ARS=130000000.00", "BTC/BOB=1350000.00", "BTC/LBP=8950000000.00"}},
			},
		}
		covered := map[string]bool{}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				src := test.Input.Source(serve(t, test.Input.Routes).URL)
				if src.Name() != test.Expect.Name {
					t.Fatalf("Name() = %q", src.Name())
				}
				covered[src.Name()] = true
				rates, err := src.Fetch(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				got := pairs(rates)
				slices.Sort(got)
				if !slices.Equal(got, test.Expect.Pairs) {
					t.Fatalf("got  %v\nwant %v", got, test.Expect.Pairs)
				}
				for _, r := range rates {
					if time.Since(r.Time) > time.Minute || r.Time.After(time.Now().Add(time.Second)) {
						t.Errorf("%s/%s has timestamp %v", r.Base, r.Quote, r.Time)
					}
					if !strings.HasPrefix(r.Source, test.Expect.Name) {
						t.Errorf("source label %q", r.Source)
					}
				}
			})
		}
		for _, s := range All(nil) {
			if !covered[s.Name()] {
				t.Errorf("source %s has no Succeed case", s.Name())
			}
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input struct {
				Routes map[string]reply
				Source func(baseURL string) Source
			}
			Expect struct {
				ErrContains string
			}
			Test struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{
				Name:   "kraken api error",
				Input:  Input{map[string]reply{"/0/public/Ticker": ok(`{"error":["EGeneral:Temporary lockout"],"result":{}}`)}, func(u string) Source { return Kraken{BaseURL: u} }},
				Expect: Expect{"EGeneral:Temporary lockout"},
			},
			{
				Name:   "kraken server error",
				Input:  Input{map[string]reply{"/0/public/Ticker": {502, "bad gateway"}}, func(u string) Source { return Kraken{BaseURL: u} }},
				Expect: Expect{"502"},
			},
			{
				Name:   "bitfinex not an array",
				Input:  Input{map[string]reply{"/v2/tickers?symbols=ALL": ok(`{"error":"ratelimit"}`)}, func(u string) Source { return Bitfinex{BaseURL: u} }},
				Expect: Expect{"cannot unmarshal"},
			},
			{
				Name:   "kucoin error code",
				Input:  Input{map[string]reply{"/api/v1/market/allTickers": ok(`{"code":"429000","msg":"Too Many Requests"}`)}, func(u string) Source { return KuCoin{BaseURL: u} }},
				Expect: Expect{"429000"},
			},
			{
				Name:   "poloniex unavailable",
				Input:  Input{map[string]reply{"/markets/price": {503, "maintenance"}}, func(u string) Source { return Poloniex{BaseURL: u} }},
				Expect: Expect{"maintenance"},
			},
			{
				Name:   "coingecko rate limited",
				Input:  Input{map[string]reply{"/api/v3/exchange_rates": {429, `{"status":{"error_code":429}}`}}, func(u string) Source { return CoinGecko{BaseURL: u} }},
				Expect: Expect{"429"},
			},
			{
				Name:   "binance blocked region",
				Input:  Input{map[string]reply{"/api/v3/ticker/bookTicker": {451, `{"code":0,"msg":"restricted location"}`}}, func(u string) Source { return Binance{BaseURL: u} }},
				Expect: Expect{"restricted location"},
			},
			{
				Name:   "bitstamp malformed json",
				Input:  Input{map[string]reply{"/api/v2/ticker/": ok(`[{"pair":`)}, func(u string) Source { return Bitstamp{BaseURL: u} }},
				Expect: Expect{"unexpected EOF"},
			},
			{
				Name: "coinbase every product down",
				Input: Input{map[string]reply{"/products/BTC-USD/ticker": {404, "NotFound"}}, func(u string) Source {
					return Coinbase{BaseURL: u, Products: []string{"BTC-USD"}}
				}},
				Expect: Expect{"404"},
			},
			{
				Name:   "bitflyer bad request",
				Input:  Input{map[string]reply{"/v1/getticker?product_code=BTC_JPY": {400, "invalid product"}}, func(u string) Source { return Bitflyer{BaseURL: u} }},
				Expect: Expect{"invalid product"},
			},
			{
				Name:   "btcmarkets server error",
				Input:  Input{map[string]reply{"/v3/markets/tickers?marketId=BTC-AUD": {500, ""}}, func(u string) Source { return BTCMarkets{BaseURL: u} }},
				Expect: Expect{"500"},
			},
			{
				Name:   "coinone error result",
				Input:  Input{map[string]reply{"/public/v2/ticker_new/KRW/BTC": ok(`{"result":"error","error_code":"107"}`)}, func(u string) Source { return Coinone{BaseURL: u} }},
				Expect: Expect{"error_code 107"},
			},
			{
				Name: "independentreserve every currency down",
				Input: Input{map[string]reply{"/Public/GetMarketSummary?primaryCurrencyCode=Xbt&secondaryCurrencyCode=Aud": {500, "down"}}, func(u string) Source {
					return IndependentReserve{BaseURL: u, Currencies: []string{"AUD"}}
				}},
				Expect: Expect{"down"},
			},
			{
				Name:   "luno unauthorized",
				Input:  Input{map[string]reply{"/api/1/tickers": {401, "unauthorized"}}, func(u string) Source { return Luno{BaseURL: u} }},
				Expect: Expect{"401"},
			},
			{
				Name:   "mercadobitcoin wrong shape",
				Input:  Input{map[string]reply{"/api/v4/tickers?symbols=BTC-BRL": ok(`{"message":"not found"}`)}, func(u string) Source { return MercadoBitcoin{BaseURL: u} }},
				Expect: Expect{"cannot unmarshal"},
			},
			{
				Name:   "paribu forbidden",
				Input:  Input{map[string]reply{"/ticker": {403, "forbidden"}}, func(u string) Source { return Paribu{BaseURL: u} }},
				Expect: Expect{"403"},
			},
			{
				Name:   "criptoya server error",
				Input:  Input{map[string]reply{"/api/btc/ars/0.1": {500, ""}}, func(u string) Source { return CriptoYa{BaseURL: u} }},
				Expect: Expect{"500"},
			},
			{
				Name:   "yadio wrong base",
				Input:  Input{map[string]reply{"/exrates": ok(fmt.Sprintf(`{"BTC":90000,"USD":{"ARS":1300},"base":"EUR","timestamp":%d}`, ms))}, func(u string) Source { return Yadio{BaseURL: u} }},
				Expect: Expect{`base "EUR"`},
			},
			{
				Name:   "yadio stale",
				Input:  Input{map[string]reply{"/exrates": ok(fmt.Sprintf(`{"BTC":100000,"USD":{"ARS":1300},"base":"USD","timestamp":%d}`, ms-3*86400000))}, func(u string) Source { return Yadio{BaseURL: u} }},
				Expect: Expect{"unusable"},
			},
			{
				Name:   "yadio without a btc price",
				Input:  Input{map[string]reply{"/exrates": ok(fmt.Sprintf(`{"USD":{"ARS":1300},"base":"USD","timestamp":%d}`, ms))}, func(u string) Source { return Yadio{BaseURL: u} }},
				Expect: Expect{"unusable"},
			},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				rates, err := test.Input.Source(serve(t, test.Input.Routes).URL).Fetch(t.Context())
				if err == nil || !strings.Contains(err.Error(), test.Expect.ErrContains) {
					t.Fatalf("got %v, want it to contain %q", err, test.Expect.ErrContains)
				}
				if len(rates) != 0 {
					t.Fatalf("returned rates along with an error: %v", pairs(rates))
				}
			})
		}
	})
}

func TestBlueLytics(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input  struct{ Body string }
			Expect struct{ Gap Gap }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{
				Name:   "numbers",
				Input:  Input{`{"oficial":{"value_sell":1010},"blue":{"value_sell":1313}}`},
				Expect: Expect{Gap{Blue: 131300, Official: 101000}},
			},
			{
				Name:   "strings with decimals",
				Input:  Input{`{"oficial":{"value_sell":"1010.5"},"blue":{"value_sell":"1313.25"},"last_update":"2026-11-07T12:00:00-03:00"}`},
				Expect: Expect{Gap{Blue: 131325, Official: 101050}},
			},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				srv := serve(t, map[string]reply{"/v2/latest": ok(test.Input.Body)})
				gap, err := BlueLytics{BaseURL: srv.URL}.Gap(t.Context())
				if err != nil || gap != test.Expect.Gap {
					t.Fatalf("got %+v, %v", gap, err)
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input  struct{ Reply reply }
			Expect struct{ ErrContains string }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "missing blue", Input: Input{ok(`{"oficial":{"value_sell":1010}}`)}, Expect: Expect{"missing sell values"}},
			{Name: "zero official", Input: Input{ok(`{"oficial":{"value_sell":0},"blue":{"value_sell":1313}}`)}, Expect: Expect{"missing sell values"}},
			{Name: "server error", Input: Input{reply{500, ""}}, Expect: Expect{"500"}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				srv := serve(t, map[string]reply{"/v2/latest": test.Input.Reply})
				if _, err := (BlueLytics{BaseURL: srv.URL}).Gap(t.Context()); err == nil || !strings.Contains(err.Error(), test.Expect.ErrContains) {
					t.Fatalf("got %v", err)
				}
			})
		}
	})
}
