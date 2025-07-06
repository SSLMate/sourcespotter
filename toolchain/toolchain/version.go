package toolchain

import (
	"fmt"
	"go/version"
	"strings"
)

func IsReproducible(goversion string) bool {
	return version.Compare(goversion, "go1.21.0") >= 0
}

func BootstrapToolchain(goversion string) string {
	// see https://go.dev/doc/install/source#go14
	if version.Compare(goversion, "go1.26.0") >= 0 {
		return "go1.24.0"
	} else if version.Compare(goversion, "go1.24.0") >= 0 {
		return "go1.22.12"
	} else if version.Compare(goversion, "go1.22.0") >= 0 {
		return "go1.20.14"
	} else if version.Compare(goversion, "go1.20") >= 0 {
		return "go1.17.13"
	} else if version.Compare(goversion, "go1.5") >= 0 {
		return "go1.4.3"
	} else {
		return ""
	}
}

type Version struct {
	GoVersion string // e.g. "go1.21.0"
	GOOS      string
	GOARCH    string
}

func (v Version) ModVersion() string {
	return fmt.Sprintf("v0.0.1-%s.%s-%s", v.GoVersion, v.GOOS, v.GOARCH)
}

func (v Version) ZipFilename() string {
	return v.ModVersion() + ".zip"
}

func ParseModVersion(modversion string) (Version, bool) {
	modversion, ok := strings.CutPrefix(modversion, "v0.0.1-")
	if !ok {
		return Version{}, false
	}
	lastdot := strings.LastIndex(modversion, ".")
	if lastdot == -1 {
		return Version{}, false
	}
	gover := modversion[:lastdot]
	if !version.IsValid(gover) {
		return Version{}, false
	}
	goos, goarch, ok := strings.Cut(modversion[lastdot+1:], "-")
	if !ok {
		return Version{}, false
	}
	return Version{
		GoVersion: gover,
		GOOS:      goos,
		GOARCH:    goarch,
	}, true
}
