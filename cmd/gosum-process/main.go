package main

import (
	"context"
	"database/sql"
	"log"
	"os"

	_ "github.com/lib/pq"
	"software.sslmate.com/src/sourcespotter/gosum/processor"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatalf("Usage: %s SUMDB", os.Args[0])
	}
	sumdbAddress := os.Args[1]
	dbspec := os.Getenv("SOURCESPOTTER_DB")
	if dbspec == "" {
		log.Fatal("$SOURCESPOTTER_DB not set")
	}

	db, err := sql.Open("postgres", dbspec)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	err = processor.Process(context.Background(), sumdbAddress, db)
	if err != nil {
		log.Fatal(err)
	}
}
