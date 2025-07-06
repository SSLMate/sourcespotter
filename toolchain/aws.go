package toolchain

import (
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
