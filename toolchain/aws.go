// Copyright (C) 2025 Opsmate, Inc.
//
// This Source Code Form is subject to the terms of the Mozilla
// Public License, v. 2.0. If a copy of the MPL was not distributed
// with this file, You can obtain one at http://mozilla.org/MPL/2.0/.
//
// This software is distributed WITHOUT A WARRANTY OF ANY KIND.
// See the Mozilla Public License for details.

package toolchain

import (
	"context"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

var (
	AWSConfig  aws.Config
	S3Bucket   string
	LambdaArch string
	LambdaFunc string
)

func newS3Client() *s3.Client {
	return s3.NewFromConfig(AWSConfig, func(opts *s3.Options) {
		opts.EndpointOptions.UseDualStackEndpoint = aws.DualStackEndpointStateEnabled
		opts.DisableLogOutputChecksumValidationSkipped = true
	})
}

func newLambdaClient() *lambda.Client {
	return lambda.NewFromConfig(AWSConfig)
}

func sourceObjectName(goversion string) string {
	return "src/" + goversion + ".src.tar.gz"
}

func toolchainObjectName(modversion string) string {
	return "toolchain/" + modversion + ".zip"
}

func logObjectName(modversion string) string {
	return "log/" + modversion + "@" + time.Now().UTC().Format(time.RFC3339)
}

func presignPutObject(ctx context.Context, objectName string) (string, error) {
	presigner := s3.NewPresignClient(newS3Client())
	presigned, err := presigner.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(S3Bucket),
		Key:    aws.String(objectName),
	}, s3.WithPresignExpires(30*time.Minute))
	if err != nil {
		return "", err
	}
	return presigned.URL, nil
}

func presignGetObject(ctx context.Context, objectName string) (string, error) {
	presigner := s3.NewPresignClient(newS3Client())
	presigned, err := presigner.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(S3Bucket),
		Key:    aws.String(objectName),
	}, s3.WithPresignExpires(30*time.Minute))
	if err != nil {
		return "", err
	}
	return presigned.URL, nil
}

func getObject(ctx context.Context, objectName string) (io.ReadCloser, error) {
	out, err := newS3Client().GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(S3Bucket),
		Key:    aws.String(objectName),
	})
	if err != nil {
		return nil, err
	}
	return out.Body, nil
}
