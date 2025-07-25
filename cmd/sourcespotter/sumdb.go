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

package main

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/lib/pq"
	"golang.org/x/sync/errgroup"
	"software.sslmate.com/src/sourcespotter"
	"software.sslmate.com/src/sourcespotter/internal/records"
	"software.sslmate.com/src/sourcespotter/internal/sths"
	"src.agwa.name/go-dbutil"
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

func monitorSumdb() {
	dbListener = pq.NewListener(sourcespotter.DBAddress, 5*time.Second, 2*time.Minute, nil)
	if err := dbListener.Listen(dbChannelName); err != nil {
		log.Fatal(err)
	}

	var enabledSumDBs []int32
	if err := dbutil.QueryAll(context.Background(), sourcespotter.DB, &enabledSumDBs, `SELECT db_id FROM db WHERE enabled ORDER BY db_id`); err != nil {
		log.Fatal(err)
	}

	group, ctx := errgroup.WithContext(context.Background())
	for _, id := range enabledSumDBs {
		log.Printf("monitoring sumdb %d...", id)

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
	log.Fatal(group.Wait())
}

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
