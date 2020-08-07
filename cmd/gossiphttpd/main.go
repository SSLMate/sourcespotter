package main

import (
	"database/sql"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"strings"

	_ "github.com/lib/pq"
	gosumgossip "software.sslmate.com/src/sourcespotter/gosum/gossip"
	"src.agwa.name/go-listener"
)

var db *sql.DB

func handleGosumGossip(w http.ResponseWriter, req *http.Request) {
	address := strings.TrimLeft(req.URL.Path, "/")
	if req.Method == http.MethodGet {
		gosumgossip.ServeGossip(address, w, req, db)
	} else if req.Method == http.MethodPost {
		gosumgossip.ReceiveGossip(address, w, req, db)
	} else {
		http.Error(w, "400 Use GET or POST please", 400)
	}
}

func main() {
	listenFlag := flag.String("listen", "", "Listen address(es)")
	flag.Parse()

	dbspec := os.Getenv("SOURCESPOTTER_DB")
	if dbspec == "" {
		log.Fatal("$SOURCESPOTTER_DB not set")
	}

	ourListeners, err := listener.OpenAll(strings.Split(*listenFlag, ","))
	if err != nil {
		log.Fatal(err)
	}
	defer listener.CloseAll(ourListeners)

	db, err = sql.Open("postgres", dbspec)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	mux := http.NewServeMux()
	mux.Handle("/gosum/", http.StripPrefix("/gosum/", http.HandlerFunc(handleGosumGossip)))

	server := http.Server{Handler: mux}

	for _, listener := range ourListeners {
		go func(listener net.Listener) {
			log.Fatal(server.Serve(listener))
		}(listener)
	}

	select {}
}
