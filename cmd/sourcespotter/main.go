package main

import (
	"context"
	"database/sql"
	"flag"
	_ "github.com/lib/pq"
	"golang.org/x/sync/errgroup"
	"log"
	"net/http"
	"software.sslmate.com/src/sourcespotter/records"
	"software.sslmate.com/src/sourcespotter/sths"
	"src.agwa.name/go-dbutil"
	"src.agwa.name/go-listener"
	"strings"
	"time"
)

var (
	db *sql.DB
)

const (
	downloadSTHInterval = 5 * time.Minute
	auditSTHInterval    = 15 * time.Minute
	ingestSleep         = 1 * time.Minute
)

func downloadSTHs(ctx context.Context, id int32) error {
	return periodically(ctx, downloadSTHInterval, func() error {
		return sths.Download(ctx, id, db)
	})
}

func auditSTHs(ctx context.Context, id int32) error {
	return periodically(ctx, auditSTHInterval, func() error {
		return sths.Audit(ctx, id, db)
	})
}

func ingestRecords(ctx context.Context, id int32) error {
	for ctx.Err() == nil {
		progressed, err := records.Ingest(ctx, id, db)
		if err != nil {
			return err
		}
		if !progressed {
			if err := sleep(ctx, ingestSleep); err != nil {
				return err
			}
		}
	}
	return ctx.Err()
}

func handleGossip(w http.ResponseWriter, req *http.Request) {
	address := strings.TrimLeft(req.URL.Path, "/")
	if req.Method == http.MethodGet {
		sths.ServeGossip(address, w, req, db)
	} else if req.Method == http.MethodPost {
		sths.ReceiveGossip(address, w, req, db)
	} else {
		http.Error(w, "400 Use GET or POST please", 400)
	}
}

func main() {
	var flags struct {
		db     string
		listen []string
	}
	flag.StringVar(&flags.db, "db", "", "Database address")
	flag.Func("listen", "Socket to listen on, in go-listener syntax (repeatable)", func(arg string) error {
		flags.listen = append(flags.listen, arg)
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

	var enabledSumDBs []int32
	if err := dbutil.QueryAll(context.Background(), db, &enabledSumDBs, `SELECT db_id FROM gosum.db WHERE enabled ORDER BY db_id`); err != nil {
		log.Fatal(err)
	}

	listeners, err := listener.OpenAll(flags.listen)
	if err != nil {
		log.Fatal(err)
	}
	defer listener.CloseAll(listeners)

	httpMux := http.NewServeMux()
	httpMux.Handle("/gossip/", http.StripPrefix("/gossip/", http.HandlerFunc(handleGossip)))
	httpServer := http.Server{
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  3 * time.Second,
		Handler:      httpMux,
	}

	group, ctx := errgroup.WithContext(context.Background())
	for _, id := range enabledSumDBs {
		id := id
		group.Go(func() error {
			return downloadSTHs(ctx, id)
		})
		group.Go(func() error {
			return auditSTHs(ctx, id)
		})
		group.Go(func() error {
			return ingestRecords(ctx, id)
		})
	}
	for _, listener := range listeners {
		listener := listener
		go func() {
			log.Fatal(httpServer.Serve(listener))
		}()
	}
	log.Fatal(group.Wait())
}

func sleep(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func periodically(ctx context.Context, interval time.Duration, f func() error) error {
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
		}
	}
}
