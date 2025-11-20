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
	"os"
	"slices"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"software.sslmate.com/src/sourcespotter"
	"software.sslmate.com/src/sourcespotter/internal/dashboard"
	"software.sslmate.com/src/sourcespotter/internal/toolchain"
	"src.agwa.name/go-listener"
	_ "src.agwa.name/go-listener/tls"
)

func main() {
	var flags struct {
		config    string
		files     string
		sumdb     bool
		toolchain bool
		telemetry bool
		listen    []string
	}
	flag.StringVar(&flags.config, "config", "", "Path to configuration file")
	flag.StringVar(&flags.files, "files", "", "Path to templates and assets to override embedded copies")
	flag.BoolVar(&flags.sumdb, "sumdb", false, "Enable sumdb monitoring")
	flag.BoolVar(&flags.toolchain, "toolchain", false, "Enable toolchain auditing")
	flag.BoolVar(&flags.telemetry, "telemetry", false, "Enable telemetry config monitoring")
	flag.Func("listen", "Run HTTP server on `LISTENER`, in go-listener syntax (repeatable)", func(arg string) error {
		flags.listen = append(flags.listen, arg)
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
		Listen    []string
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
	sourcespotter.DBAddress = cfg.Database
	if db, err := sql.Open("postgres", cfg.Database); err == nil {
		sourcespotter.DB = db
	} else {
		log.Fatal(err)
	}
	defer sourcespotter.DB.Close()

	if flags.files != "" {
		dashboard.Files = os.DirFS(flags.files)
	}

	if awsCfg, err := config.LoadDefaultConfig(context.Background()); err == nil {
		toolchain.AWSConfig = awsCfg
	} else {
		log.Fatal(err)
	}
	toolchain.Bucket = cfg.Toolchain.Bucket
	toolchain.BootstrapToolchain = cfg.Toolchain.BootstrapToolchain
	toolchain.BootstrapHash = cfg.Toolchain.BootstrapHash
	toolchain.LambdaArch = cfg.Toolchain.LambdaArch
	toolchain.LambdaFunc = cfg.Toolchain.LambdaFunc

	if listen := slices.Concat(cfg.Listen, flags.listen); len(listen) > 0 {
		listeners, err := listener.OpenAll(listen)
		if err != nil {
			log.Fatal(err)
		}
		defer listener.CloseAll(listeners)
		server := newHTTPServer()
		for i, listener := range listeners {
			go func() {
				log.Printf("serving HTTP on %s...", listen[i])
				log.Fatal(server.Serve(listener))
			}()
		}
	}
	if flags.sumdb {
		go monitorSumdb()
	}
	if flags.toolchain {
		go auditToolchains()
	}
	if flags.telemetry {
		go refreshTelemetryCounters()
	}

	select {}
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
