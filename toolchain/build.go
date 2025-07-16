// Package toolchain contains functions for building a Go toolchain
package toolchain

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

type BuildInput struct {
	// WorkDir is the path to an empty directory in which to do work
	WorkDir string

	// Version is the version of the toolchain to build
	Version Version

	// GetSource returns the source tar.gz file for the given Go version, e.g. "go1.24.4"
	GetSource func(context.Context, string) (io.ReadCloser, error)

	// GetToolchain, if non-nil, returns a pre-built toolchain zip file for the given version, or nil, nil if a pre-built toolchain is not available
	GetToolchain func(context.Context, Version) (io.ReadCloser, error)

	// Log, if non-nil, receives the output the build scripts
	Log io.Writer
}

// Build builds a Go toolchain and returns the path to the module zip file, which will be under input.WorkDir. Build will retrieve or build other toolchains (using input.GetToolchain and input.GetSource) as necessary for bootstrapping.
func Build(ctx context.Context, input *BuildInput) (string, error) {
	return input.build(ctx)
}

func (b *BuildInput) build(ctx context.Context) (string, error) {
	gorootBootstrap, err := b.prepareBootstrap(ctx, b.Version.GoVersion)
	if err != nil {
		return "", err
	}

	b.logf("Getting source for %s...", b.Version.GoVersion)
	goroot := filepath.Join(b.WorkDir, "goroot")
	if err := b.getSource(ctx, b.Version.GoVersion, goroot); err != nil {
		return "", fmt.Errorf("error getting source for %s: %w", b.Version.GoVersion, err)
	}

	b.logf("Building %s using %q...", b.Version.GoVersion, gorootBootstrap)
	env := []string{
		"GOROOT_BOOTSTRAP=" + gorootBootstrap,
		"GOOS=" + b.Version.GOOS,
		"GOARCH=" + b.Version.GOARCH,
	}
	if b.Version.GOOS == "linux" && b.Version.GOARCH == "arm" {
		env = append(env, "GOARM=6")
	}
	if err := b.buildSource(ctx, goroot, []string{"-distpack"}, env); err != nil {
		return "", fmt.Errorf("error building source for %s: %w", b.Version.GoVersion, err)
	}
	zippath := filepath.Join(goroot, "pkg", "distpack", b.Version.ZipFilename())
	return zippath, nil
}

func (b *BuildInput) prepareBootstrap(ctx context.Context, goversion string) (string, error) {
	bootstrapVersion := BootstrapToolchain(goversion)
	if bootstrapVersion == "" {
		// only need C compiler
		return "", nil
	}

	// see if there is a pre-built toolchain for bootstrapVersion
	if gorootBootstrap, err := b.installBootstrapToolchain(ctx, bootstrapVersion); err != nil {
		return "", fmt.Errorf("error installing bootstrap toolchain %s: %w", bootstrapVersion, err)
	} else if gorootBootstrap != "" {
		return gorootBootstrap, nil
	}

	// no pre-built toolchain; need to build bootstrapVersion from source
	gorootBootstrap2, err := b.prepareBootstrap(ctx, bootstrapVersion)
	if err != nil {
		return "", err
	}

	b.logf("Building %s using %q...", bootstrapVersion, gorootBootstrap2)
	gorootBootstrap := filepath.Join(b.WorkDir, bootstrapVersion)
	if err := b.getSource(ctx, bootstrapVersion, gorootBootstrap); err != nil {
		return "", fmt.Errorf("error getting source for %s: %w", bootstrapVersion, err)
	}
	if err := b.buildSource(ctx, gorootBootstrap, nil, []string{
		"GOROOT_BOOTSTRAP=" + gorootBootstrap2,
	}); err != nil {
		return "", fmt.Errorf("error building source for %s (using %q for bootstrap): %w", bootstrapVersion, gorootBootstrap2, err)
	}
	return gorootBootstrap, nil
}

func (b *BuildInput) installBootstrapToolchain(ctx context.Context, bootstrapVersion string) (string, error) {
	version := Version{GoVersion: bootstrapVersion, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}
	zipBytes, err := b.getToolchain(ctx, version)
	if err != nil {
		return "", fmt.Errorf("error downloading bootstrap toolchain: %w", err)
	} else if zipBytes == nil {
		return "", nil
	}
	b.logf("Installing %s to use for bootstrap...", version.ModVersion())
	zipReader, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return "", fmt.Errorf("error reading bootstrap toolchain zip: %w", err)
	}
	fsys, err := fs.Sub(zipReader, "golang.org/toolchain@"+version.ModVersion())
	if err != nil {
		return "", fmt.Errorf("error making bootstrap toolchain filesystem: %w", err)
	}
	bootstrapDir := filepath.Join(b.WorkDir, bootstrapVersion)
	if err := os.CopyFS(bootstrapDir, fsys); err != nil {
		return "", fmt.Errorf("error unzipping bootstrap toolchain: %w", err)
	}
	return bootstrapDir, nil
}

func (b *BuildInput) getSource(ctx context.Context, goVersion string, destDir string) error {
	f, err := b.GetSource(ctx, goVersion)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		filename := path.Clean(hdr.Name)
		filename, ok := strings.CutPrefix(filename, "go/")
		if !ok {
			continue
		}
		target := filepath.Join(destDir, filepath.FromSlash(filename))
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(hdr.Mode)); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0777); err != nil {
				return err
			}
			w, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR, os.FileMode(hdr.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(w, tr); err != nil {
				w.Close()
				return err
			}
			w.Close()
		}
	}
	return nil
}

func (b *BuildInput) buildSource(ctx context.Context, goroot string, args []string, env []string) error {
	dir := filepath.Join(goroot, "src")
	cmd := exec.CommandContext(ctx, "./make.bash", args...)
	cmd.Dir = dir
	cmd.Env = []string{
		// standard environment variables
		"LANG=C",
		"PATH=/usr/local/bin:/usr/bin:/bin",
		"SHELL=/bin/sh",
		"PWD=" + dir,

		// environment variables that affect the toolchain build
		"CC=gcc -no-pie",
		"CGO_ENABLED=0",
		"GOTOOLCHAIN=local",
	}
	for _, name := range []string{"USER", "LOGNAME", "HOME", "TMPDIR"} {
		if value, ok := os.LookupEnv(name); ok {
			cmd.Env = append(cmd.Env, name+"="+value)
		}
	}
	cmd.Env = append(cmd.Env, env...)
	cmd.Stdout = b.Log
	cmd.Stderr = b.Log
	return cmd.Run()
}

func (b *BuildInput) getToolchain(ctx context.Context, version Version) ([]byte, error) {
	if b.GetToolchain == nil {
		return nil, nil
	}
	f, err := b.GetToolchain(ctx, version)
	if err != nil {
		return nil, err
	} else if f == nil {
		return nil, nil
	}
	defer f.Close()
	return io.ReadAll(f)
}

func (b *BuildInput) logf(format string, a ...any) {
	if b.Log != nil {
		fmt.Fprintf(b.Log, format+"\n", a...)
	}
}
