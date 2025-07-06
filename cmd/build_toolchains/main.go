package main

import (
	"context"
	"database/sql"
	"flag"
	"log"

	"github.com/aws/aws-sdk-go-v2/config"
	_ "github.com/lib/pq"

	"software.sslmate.com/src/sourcespotter/toolchain"
)

func main() {
	var flags struct {
		db         string
		s3bucket   string
		lambdaArch string
		lambdaFunc string
	}
	flag.StringVar(&flags.db, "db", "", "Database address")
	flag.StringVar(&flags.s3bucket, "s3-bucket", "", "S3 bucket for artifacts")
	flag.StringVar(&flags.lambdaArch, "lambda-arch", "", "Lambda architecture")
	flag.StringVar(&flags.lambdaFunc, "lambda-func", "", "Lambda function name")
	flag.Parse()

	if flags.db == "" {
		log.Fatal("-db flag not provided")
	}
	if flags.s3bucket == "" {
		log.Fatal("-s3-bucket flag not provided")
	}

	db, err := sql.Open("postgres", flags.db)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	toolchain.AWSConfig = cfg
	toolchain.S3Bucket = flags.s3bucket
	toolchain.LambdaArch = flags.lambdaArch
	toolchain.LambdaFunc = flags.lambdaFunc

	if err := toolchain.BuildAll(context.Background(), db); err != nil {
		log.Fatal(err)
	}
}
