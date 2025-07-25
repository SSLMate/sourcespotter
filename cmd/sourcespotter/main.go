// Copyright (C) 2025 Opsmate, Inc.
//
// Permission is hereby granted, free of charge, to any person obtaining a
// copy of this software and associated documentation files (the "Software"),
// to deal in the Software without restriction, including without limitation
// the rights to use, copy, modify, merge, publish, distribute, sublicense,
// and/or sell copies of the Software, and to permit persons to whom the
// Software is furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included
// in all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL
// THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR
// OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE,
// ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR
// OTHER DEALINGS IN THE SOFTWARE.
//
// Except as contained in this notice, the name(s) of the above copyright
// holders shall not be used in advertising or otherwise to promote the
// sale, use or other dealings in this Software without prior written
// authorization.

// sourcespotter is a daemon that continuously audits the Go checksum database
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/lib/pq"
	"golang.org/x/sync/errgroup"
	"software.sslmate.com/src/sourcespotter"
	"software.sslmate.com/src/sourcespotter/internal/dashboard"
	"software.sslmate.com/src/sourcespotter/internal/records"
	"software.sslmate.com/src/sourcespotter/internal/sths"
	"software.sslmate.com/src/sourcespotter/internal/toolchain"
	"src.agwa.name/go-dbutil"
	"src.agwa.name/go-listener"
	_ "src.agwa.name/go-listener/tls"
	"src.agwa.name/go-util/logfilter"
)

const (
	downloadSTHInterval = 1 * time.Minute
	auditSTHInterval    = 15 * time.Minute * 10
	ingestSleep         = 5 * time.Minute * 10
	dbChannelName       = `events`
)

var (
	dbListener   *pq.Listener
	sumdbSignals = make(map[int32]signals)
)

type signals struct {
	newSTH      signal
	newPosition signal
}

func makeSignals() signals {
	return signals{
		newSTH:      makeSignal(),
		newPosition: makeSignal(),
	}
}

type signal chan struct{}

func makeSignal() signal {
	return make(chan struct{}, 1)
}

func (s signal) raise() {
	select {
	case s <- struct{}{}:
	default:
	}
}

func downloadSTHs(ctx context.Context, id int32) error {
	for {
		if err := sths.Download(ctx, id); err != nil {
			return err
		}
		if err := sleep(ctx, downloadSTHInterval, nil); err != nil {
			return err
		}
	}
}

func auditSTHs(ctx context.Context, id int32, newPositionSignal <-chan struct{}) error {
	for {
		if err := sths.Audit(ctx, id); err != nil {
			return err
		}
		if err := sleep(ctx, auditSTHInterval, newPositionSignal); err != nil {
			return err
		}
	}
}

func ingestRecords(ctx context.Context, id int32, newSTHSignal <-chan struct{}) error {
	for {
		if _, err := records.Ingest(ctx, id); err != nil {
			return err
		}
		if err := sleep(ctx, ingestSleep, newSTHSignal); err != nil {
			return err
		}
	}
}

func handleNotifications(ctx context.Context) error {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			// ping server to force a reconnection if connection is broken
			dbListener.Ping()
		case n := <-dbListener.Notify:
			handleNotification(n)
		}
	}
}

func handleNotification(n *pq.Notification) {
	if n == nil {
		// Database connection was re-established, so we may have missed notifications
		// Don't do anything; just wait for stuff to happen on its normal schedule
		return
	}

	var payload struct {
		DBID  int32
		Event string
	}
	if err := json.Unmarshal([]byte(n.Extra), &payload); err != nil {
		log.Printf("Ignoring malformed database notification: %s", err)
		return
	}

	signals, ok := sumdbSignals[payload.DBID]
	if !ok {
		log.Printf("Ignoring database notification for unknown sumdb %d", payload.DBID)
		return
	}

	switch payload.Event {
	case "new_sth":
		signals.newSTH.raise()
	case "new_position":
		signals.newPosition.raise()
	default:
		log.Printf("Ignoring database notification with unknown event %q", payload.Event)
	}
}

func handleGossip(w http.ResponseWriter, req *http.Request) {
	address := strings.TrimLeft(req.URL.Path, "/")
	if address == "" {
		dashboard.ServeHTTP(w, req)
	} else if req.Method == http.MethodGet {
		sths.ServeGossip(address, w, req)
	} else if req.Method == http.MethodPost {
		sths.ReceiveGossip(address, w, req)
	} else {
		http.Error(w, "400 Use GET or POST please", 400)
	}
}

func main() {
	var flags struct {
		config       string
		listenGossip []string
	}
	flag.StringVar(&flags.config, "config", "", "Path to configuration file")
	flag.Func("listen-gossip", "Socket for gossip server, in go-listener syntax (repeatable)", func(arg string) error {
		flags.listenGossip = append(flags.listenGossip, arg)
		return nil
	})
	flag.Parse()

	if flags.config == "" {
		log.Fatal("-config flag not provided")
	}

	configData, err := os.ReadFile(flags.config)
	if err != nil {
		log.Fatal(err)
	}
	var cfg struct {
		Domain    string
		Database  string
		Toolchain struct {
			Bucket             string
			BootstrapToolchain string
			BootstrapHash      string
			LambdaArch         string
			LambdaFunc         string
		}
	}
	if err := json.Unmarshal(configData, &cfg); err != nil {
		log.Fatal(err)
	}

	sourcespotter.Domain = cfg.Domain

	if db, err := sql.Open("postgres", cfg.Database); err == nil {
		sourcespotter.DB = db
	} else {
		log.Fatal(err)
	}
	defer sourcespotter.DB.Close()

	dbListener = pq.NewListener(cfg.Database, 5*time.Second, 2*time.Minute, nil)
	if err := dbListener.Listen(dbChannelName); err != nil {
		log.Fatal(err)
	}

	awsCfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	toolchain.AWSConfig = awsCfg
	toolchain.Bucket = cfg.Toolchain.Bucket
	toolchain.BootstrapToolchain = cfg.Toolchain.BootstrapToolchain
	toolchain.BootstrapHash = cfg.Toolchain.BootstrapHash
	toolchain.LambdaArch = cfg.Toolchain.LambdaArch
	toolchain.LambdaFunc = cfg.Toolchain.LambdaFunc

	var enabledSumDBs []int32
	if err := dbutil.QueryAll(context.Background(), sourcespotter.DB, &enabledSumDBs, `SELECT db_id FROM db WHERE enabled ORDER BY db_id`); err != nil {
		log.Fatal(err)
	}

	gossipListeners, err := listener.OpenAll(flags.listenGossip)
	if err != nil {
		log.Fatal(err)
	}
	defer listener.CloseAll(gossipListeners)

	gossipServer := http.Server{
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  3 * time.Second,
		Handler:      http.HandlerFunc(handleGossip),
		ErrorLog:     logfilter.New(log.Default(), logfilter.HTTPServerErrors),
	}

	group, ctx := errgroup.WithContext(context.Background())
	for _, id := range enabledSumDBs {
		signals := makeSignals()
		sumdbSignals[id] = signals
		group.Go(func() error {
			return downloadSTHs(ctx, id)
		})
		group.Go(func() error {
			return auditSTHs(ctx, id, signals.newPosition)
		})
		group.Go(func() error {
			return ingestRecords(ctx, id, signals.newSTH)
		})
	}
	group.Go(func() error {
		return handleNotifications(ctx)
	})
	for _, listener := range gossipListeners {
		go func() {
			log.Fatal(gossipServer.Serve(listener))
		}()
	}
	log.Fatal(group.Wait())
}

func sleep(ctx context.Context, duration time.Duration, wakeup <-chan struct{}) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	case <-wakeup:
		return nil
	}
}
