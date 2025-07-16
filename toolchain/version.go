package toolchain

import (
	"fmt"
	"go/version"
	"strings"
)

// FirstReproducibleVersion is the first version of Go that was reproducible
const FirstReproducibleVersion = "go1.21.0"

// IsReproducible reports if goversion is supposed to be reproducible
func IsReproducible(goversion string) bool {
	return version.Compare(goversion, FirstReproducibleVersion) >= 0
}

// BootstrapToolchain returns the version of Go needed to bootstrap goversion, or the empty string if a C compiler is needed
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

// Version uniquely identifies a built toolchain
type Version struct {
	GoVersion string // e.g. "go1.21.0"
	GOOS      string
	GOARCH    string
}

// ModVersion returns the corresponding golang.org/toolchain module version
func (v Version) ModVersion() string {
	return fmt.Sprintf("v0.0.1-%s.%s-%s", v.GoVersion, v.GOOS, v.GOARCH)
}

// ZipFilename returns the filename of the golang.org/toolchain module version
func (v Version) ZipFilename() string {
	return v.ModVersion() + ".zip"
}

// ParseModVersion parses a golang.org/toolchain module version
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
