package main

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/branthz/mesh"
	"github.com/branthz/utarrow/lib/log"
)

func init() {
	err := log.Setup("./xxx.log", "debug")
	if err != nil {
		fmt.Println("init:", err)
		os.Exit(-1)
	}
}

func main() {
	peers := &stringset{}
	var (
		httpListen = flag.String("http", ":8080", "HTTP listen address")
		meshListen = flag.String("mesh", net.JoinHostPort("0.0.0.0", strconv.Itoa(mesh.Port)), "mesh listen address")
		hwaddr     = flag.String("hwaddr", mustHardwareAddr(), "MAC address, i.e. mesh peer ID")
		nickname   = flag.String("nickname", mustHostname(), "peer nickname")
		password   = flag.String("password", "", "password (optional)")
		channel    = flag.String("channel", "default", "gossip channel name")
	)
	flag.Var(peers, "peer", "initial peer (may be repeated)")
	flag.Parse()

	host, portStr, err := net.SplitHostPort(*meshListen)
	if err != nil {
		log.Fatal("mesh address: %s: %v", *meshListen, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		log.Fatal("mesh address: %s: %v", *meshListen, err)
	}

	name, err := mesh.PeerNameFromString(*hwaddr)
	if err != nil {
		log.Fatal("%s: %v", *hwaddr, err)
	}

	router, err := mesh.NewRouter(mesh.Config{
		Host:               host,
		Port:               port,
		ProtocolMinVersion: mesh.ProtocolMinVersion,
		Password:           []byte(*password),
		ConnLimit:          64,
		PeerDiscovery:      true,
		TrustedSubnets:     []*net.IPNet{},
	}, name, *nickname, mesh.NullOverlay{})

	if err != nil {
		log.Fatal("Could not create router: %v", err)
	}

	peer := newPeer(name)
	gossip, err := router.NewGossip(*channel, peer)
	if err != nil {
		log.Fatal("Could not create gossip: %v", err)
	}

	peer.register(gossip)

	func() {
		log.Info("mesh router starting (%s)", *meshListen)
		router.Start()
	}()
	defer func() {
		log.Info("mesh router stopping")
		router.Stop()
	}()

	router.ConnectionMaker.InitiateConnections(peers.slice(), true)

	errs := make(chan error)
	go func() {
		c := make(chan os.Signal)
		signal.Notify(c, syscall.SIGINT)
		errs <- fmt.Errorf("%s", <-c)
	}()
	go func() {
		log.Info("HTTP server starting (%s)", *httpListen)
		http.HandleFunc("/", handle(peer))
		errs <- http.ListenAndServe(*httpListen, nil)
	}()
	log.Infoln(<-errs)
}

type counter interface {
	get() int
	incr() int
}

func handle(c counter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			fmt.Fprintf(w, "get => %d\n", c.get())

		case "POST":
			fmt.Fprintf(w, "incr => %d\n", c.incr())
		}
	}
}

type stringset map[string]struct{}

func (ss stringset) Set(value string) error {
	ss[value] = struct{}{}
	return nil
}

func (ss stringset) String() string {
	return strings.Join(ss.slice(), ",")
}

func (ss stringset) slice() []string {
	slice := make([]string, 0, len(ss))
	for k := range ss {
		slice = append(slice, k)
	}
	sort.Strings(slice)
	return slice
}

func mustHardwareAddr() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		panic(err)
	}
	for _, iface := range ifaces {
		if s := iface.HardwareAddr.String(); s != "" {
			return s
		}
	}
	panic("no valid network interfaces")
}

func mustHostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		panic(err)
	}
	return hostname
}
