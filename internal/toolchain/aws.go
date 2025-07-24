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

func presignPutObject(ctx context.Context, objectName string, contentType string) (string, error) {
	presigner := s3.NewPresignClient(newS3Client())
	presigned, err := presigner.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(S3Bucket),
		Key:    aws.String(objectName),
		ContentType:    aws.String(contentType),
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

func deleteObject(ctx context.Context, objectName string) error {
	_, err := newS3Client().DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(S3Bucket),
		Key:    aws.String(objectName),
	})
	return err
}
