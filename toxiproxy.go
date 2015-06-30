package main

import (
	"flag"
	"math/rand"
	"time"

	"github.com/Shopify/toxiproxy/proxy"
)

var host string
var port string

func init() {
	flag.StringVar(&host, "host", "localhost", "Host for toxiproxy's API to listen on")
	flag.StringVar(&port, "port", "8474", "Port for toxiproxy's API to listen on")
	seed := flag.Int64("seed", time.Now().UTC().UnixNano(), "Seed for randomizing toxics with")
	flag.Parse()
	rand.Seed(*seed)
}

func main() {
	proxies := proxy.NewProxyCollection()
	server := proxy.NewServer(proxies)
	server.Listen(host, port)
}
