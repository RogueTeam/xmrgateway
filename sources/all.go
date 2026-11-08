package sources

// All returns every source with default settings sharing one client.
func All(c HTTPClient) []Source {
	return []Source{
		Kraken{Client: c},
		Bitfinex{Client: c},
		KuCoin{Client: c},
		Poloniex{Client: c},
		CoinGecko{Client: c},
		Binance{Client: c},
		Bitstamp{Client: c},
		Coinbase{Client: c},
		Bitflyer{Client: c},
		BTCMarkets{Client: c},
		Coinone{Client: c},
		IndependentReserve{Client: c},
		Luno{Client: c},
		MercadoBitcoin{Client: c},
		Paribu{Client: c},
		CriptoYa{Client: c},
		Yadio{Client: c},
	}
}
