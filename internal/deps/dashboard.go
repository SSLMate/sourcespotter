package deps

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"software.sslmate.com/src/sourcespotter"
	basedashboard "software.sslmate.com/src/sourcespotter/internal/dashboard"
)

var platforms = []string{
	"aix/ppc64",
	"android/386",
	"android/amd64",
	"android/arm",
	"android/arm64",
	"darwin/amd64",
	"darwin/arm64",
	"dragonfly/amd64",
	"freebsd/386",
	"freebsd/amd64",
	"freebsd/arm",
	"freebsd/arm64",
	"freebsd/riscv64",
	"illumos/amd64",
	"ios/amd64",
	"ios/arm64",
	"js/wasm",
	"linux/386",
	"linux/amd64",
	"linux/arm",
	"linux/arm64",
	"linux/loong64",
	"linux/mips",
	"linux/mips64",
	"linux/mips64le",
	"linux/mipsle",
	"linux/ppc64",
	"linux/ppc64le",
	"linux/riscv64",
	"linux/s390x",
	"netbsd/386",
	"netbsd/amd64",
	"netbsd/arm",
	"netbsd/arm64",
	"openbsd/386",
	"openbsd/amd64",
	"openbsd/arm",
	"openbsd/arm64",
	"openbsd/ppc64",
	"openbsd/riscv64",
	"plan9/386",
	"plan9/amd64",
	"plan9/arm",
	"solaris/amd64",
	"wasip1/wasm",
	"windows/386",
	"windows/amd64",
	"windows/arm64",
}

type formData struct {
	Package  string
	Platform string
	Test     bool
	Tags     string
}

type moduleResult struct {
	Module   string
	Packages []string
}

type dashboardData struct {
	Domain    string
	Form      formData
	Platforms []string
	Results   []moduleResult
	Error     string
}

func ServeDashboard(w http.ResponseWriter, req *http.Request) {
	form := parseForm(req)

	data := &dashboardData{
		Domain:    sourcespotter.Domain,
		Form:      form,
		Platforms: platforms,
	}

	if form.Package != "" {
		results, err := listDependencies(req.Context(), form)
		data.Results = results
		if err != nil {
			data.Error = err.Error()
		}
	}

	basedashboard.ServePage(w, req,
		"Go Package Dependencies - Source Spotter",
		"List the dependencies of a Go package, properly.",
		"deps.html", data)
}

func parseForm(req *http.Request) formData {
	q := req.URL.Query()
	form := formData{
		Package:  strings.TrimSpace(q.Get("package")),
		Platform: strings.TrimSpace(q.Get("platform")),
		Tags:     strings.TrimSpace(q.Get("tags")),
		Test:     q.Get("test") == "1",
	}
	if form.Platform == "" || !slices.Contains(platforms, form.Platform) {
		form.Platform = "linux/amd64"
	}
	return form
}

func listDependencies(ctx context.Context, form formData) ([]moduleResult, error) {
	if sourcespotter.GOPATH == "" {
		return nil, fmt.Errorf("Source Spotter GOPATH not configured")
	}

	goos, goarch, _ := strings.Cut(form.Platform, "/")

	tempDir, err := os.MkdirTemp("", "sourcespotter-deps-")
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	if err := os.WriteFile(filepath.Join(tempDir, "go.mod"), []byte("module tmp\ngo 1.25.3\n"), 0666); err != nil {
		return nil, fmt.Errorf("failed to write go.mod: %w", err)
	}

	args := []string{"list", "-mod=mod", "-modcacherw", "-buildvcs=false", "-deps", "-f", "{{if .Module}}{{.Module.Path}}@{{.Module.Version}} {{.ImportPath}}{{end}}"}
	if form.Test {
		args = append(args, "-test")
	}
	if form.Tags != "" {
		args = append(args, "-tags", form.Tags)
	}
	args = append(args, "--", form.Package)

	var stdout, stderr bytes.Buffer

	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = tempDir
	cmd.Env = append([]string{},
		"PATH=/usr/local/bin:/usr/bin/:/bin",
		"PWD="+tempDir,
		"USER="+os.Getenv("USER"),
		"LOGNAME="+os.Getenv("LOGNAME"),
		"HOME="+os.Getenv("HOME"),
		"CGO_ENABLED=0",
		"GOPATH="+sourcespotter.GOPATH,
		"GOVCS=*:off",
		"GOOS="+goos,
		"GOARCH="+goarch,
	)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			return nil, errors.New(strings.TrimSpace(stderr.String()))
		} else {
			return nil, fmt.Errorf("failed to run go list: %w", err)
		}
	}

	deps := make(map[string][]string)
	for scanner := bufio.NewScanner(&stdout); scanner.Scan(); {
		module, pkg, _ := strings.Cut(scanner.Text(), " ")
		deps[module] = append(deps[module], pkg)
	}
	results := make([]moduleResult, 0, len(deps))
	for _, module := range slices.Sorted(maps.Keys(deps)) {
		packages := deps[module]
		slices.Sort(packages)
		results = append(results, moduleResult{Module: module, Packages: packages})
	}
	return results, nil
}
