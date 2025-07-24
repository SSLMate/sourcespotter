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
	"database/sql"
	"flag"
	"log"

	"github.com/aws/aws-sdk-go-v2/config"
	_ "github.com/lib/pq"

	"software.sslmate.com/src/sourcespotter/internal/toolchain"
)

func main() {
	var flags struct {
		db          string
		s3bucket    string
		lambdaArch  string
		lambdaFunc  string
		go120Object string
		go120Hash   string
	}
	flag.StringVar(&flags.db, "db", "", "Database address")
	flag.StringVar(&flags.s3bucket, "s3-bucket", "", "S3 bucket for artifacts")
	flag.StringVar(&flags.lambdaArch, "lambda-arch", "", "Lambda architecture")
	flag.StringVar(&flags.lambdaFunc, "lambda-func", "", "Lambda function name")
	flag.StringVar(&flags.go120Object, "go120-object", "", "S3 object containing Go 1.20 bootstrap toolchain zip")
	flag.StringVar(&flags.go120Hash, "go120-hash", "", "dirhash of Go 1.20 bootstrap toolchain zip")
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
	toolchain.Go120Object = flags.go120Object
	toolchain.Go120Hash = flags.go120Hash

	//if err := toolchain.AuditAll(context.Background(), db); err != nil {
	if err := toolchain.Audit(context.Background(), db, flag.Arg(0)); err != nil {
		log.Fatal(err)
	}
}
