// Copyright (C) 2023 Opsmate, Inc.
//
// This Source Code Form is subject to the terms of the Mozilla
// Public License, v. 2.0. If a copy of the MPL was not distributed
// with this file, You can obtain one at http://mozilla.org/MPL/2.0/.
//
// This software is distributed WITHOUT A WARRANTY OF ANY KIND.
// See the Mozilla Public License for details.

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"github.com/lib/pq"
	"golang.org/x/sync/errgroup"
	"html/template"
	"log"
	"net/http"
	"software.sslmate.com/src/sourcespotter/dashboard"
	"software.sslmate.com/src/sourcespotter/records"
	"software.sslmate.com/src/sourcespotter/sths"
	"src.agwa.name/go-dbutil"
	"src.agwa.name/go-listener"
	_ "src.agwa.name/go-listener/tls"
	"src.agwa.name/go-util/logfilter"
	"strings"
	"time"
)

const (
	downloadSTHInterval = 1 * time.Minute
	auditSTHInterval    = 15 * time.Minute * 10
	ingestSleep         = 5 * time.Minute * 10
	dbChannelName       = `events`
)

var (
	db                *sql.DB
	dbListener        *pq.Listener
	dashboardTemplate *template.Template
	sumdbSignals      = make(map[int32]signals)
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
	return periodically(ctx, downloadSTHInterval, nil, func() error {
		return sths.Download(ctx, id, db)
	})
}

func auditSTHs(ctx context.Context, id int32, newPositionSignal <-chan struct{}) error {
	return periodically(ctx, auditSTHInterval, newPositionSignal, func() error {
		return sths.Audit(ctx, id, db)
	})
}

func ingestRecords(ctx context.Context, id int32, newSTHSignal <-chan struct{}) error {
	for ctx.Err() == nil {
		if _, err := records.Ingest(ctx, id, db); err != nil {
			return err
		}
		if err := sleep(ctx, ingestSleep, newSTHSignal); err != nil {
			return err
		}
	}
	return ctx.Err()
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
		dashboard.ServeHTTP(w, req, db, dashboardTemplate)
	} else if req.Method == http.MethodGet {
		sths.ServeGossip(address, w, req, db)
	} else if req.Method == http.MethodPost {
		sths.ReceiveGossip(address, w, req, db)
	} else {
		http.Error(w, "400 Use GET or POST please", 400)
	}
}

func main() {
	var flags struct {
		db                string
		dashboardTemplate string
		listenGossip      []string
	}
	flag.StringVar(&flags.db, "db", "", "Database address")
	flag.StringVar(&flags.dashboardTemplate, "dashboard-template", "", "Path to alternative dashboard template")
	flag.Func("listen-gossip", "Socket for gossip server, in go-listener syntax (repeatable)", func(arg string) error {
		flags.listenGossip = append(flags.listenGossip, arg)
		return nil
	})
	flag.Parse()

	if flags.db == "" {
		log.Fatal("-db flag not provided")
	}

	if ret, err := sql.Open("postgres", flags.db); err == nil {
		db = ret
	} else {
		log.Fatal(err)
	}
	defer db.Close()

	dbListener = pq.NewListener(flags.db, 5*time.Second, 2*time.Minute, nil)
	if err := dbListener.Listen(dbChannelName); err != nil {
		log.Fatal(err)
	}

	if flags.dashboardTemplate != "" {
		if ret, err := template.ParseFiles(flags.dashboardTemplate); err == nil {
			dashboardTemplate = ret
		} else {
			log.Fatal(err)
		}
	}

	var enabledSumDBs []int32
	if err := dbutil.QueryAll(context.Background(), db, &enabledSumDBs, `SELECT db_id FROM db WHERE enabled ORDER BY db_id`); err != nil {
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
	group.Go(func() error {
		return handleNotifications(ctx)
	})
	for _, id := range enabledSumDBs {
		id := id
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
	for _, listener := range gossipListeners {
		listener := listener
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

func periodically(ctx context.Context, interval time.Duration, wakeup <-chan struct{}, f func() error) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	if err := f(); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := f(); err != nil {
				return err
			}
		case <-wakeup:
			if err := f(); err != nil {
				return err
			}
			ticker.Reset(interval)
		}
	}
}
