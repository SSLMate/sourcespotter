// Lambda function to build a Go toolchain and upload it to S3
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"

	"github.com/aws/aws-lambda-go/lambda"
	"software.sslmate.com/src/sourcespotter/toolchain"
)

func init() {
	if output, err := exec.Command("sudo", "apt-get", "update").CombinedOutput(); err != nil {
		panic(fmt.Errorf("apt-get update: %w: %s", err, bytes.TrimSpace(output)))
	}
	if output, err := exec.Command("sudo", "apt-get", "install", "-y", "gcc", "libc6-dev").CombinedOutput(); err != nil {
		panic(fmt.Errorf("apt-get install: %w: %s", err, bytes.TrimSpace(output)))
	}
}

type Event struct {
	Version       toolchain.Version
	SourceURLs    map[string]string
	ToolchainURLs map[string]string
	ZipUploadURL  string
	LogUploadURL  string
}

func handler(ctx context.Context, event Event) error {
	workDir, err := os.MkdirTemp("", "build-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(workDir)

	var logBuf bytes.Buffer

	input := &toolchain.BuildInput{
		WorkDir:   workDir,
		Version:   event.Version,
		GetSource: func(ctx context.Context, version string) (io.ReadCloser, error) {
			url, ok := event.SourceURLs[version]
			if !ok {
				return nil, fmt.Errorf("no URL provided for source %q in the Lambda input", version)
			}
			return download(ctx, url)
		},
		GetToolchain: func(ctx context.Context, version toolchain.Version) (io.ReadCloser, error) {
			url, ok := event.ToolchainURLs[version.ModVersion()]
			if !ok {
				return nil, nil
			}
			return download(ctx, url)
		},
		Log: &logBuf,
	}

	var errs []error
	if zipPath, err := toolchain.Build(ctx, input); err != nil {
		errs = append(errs, fmt.Errorf("build failed: %w", err))
	} else if err := uploadFile(ctx, event.ZipUploadURL, zipPath); err != nil {
		errs = append(errs, fmt.Errorf("uploading zip failed: %w", err))
	}

	if err := upload(ctx, event.LogUploadURL, &logBuf); err != nil {
		errs = append(errs, fmt.Errorf("uploading log failed: %w", err))
	}

	return errors.Join(errs...)
}

func download(ctx context.Context, downloadURL string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, &url.Error{Op: "Get", URL: downloadURL, Err: fmt.Errorf("%s: %s", resp.Status, bytes.TrimSpace(respBody))}
	}
	return resp.Body, nil
}

func upload(ctx context.Context, uploadURL string, body io.Reader) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL, body)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	respBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &url.Error{Op: "Put", URL: uploadURL, Err: fmt.Errorf("%s: %s", resp.Status, bytes.TrimSpace(respBody))}
	}
	return nil
}

func uploadFile(ctx context.Context, url, path string) error {
	f, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return upload(ctx, url, bytes.NewReader(f))
}

func main() {
	lambda.Start(handler)
}
