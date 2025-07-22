package lambda

import (
	"software.sslmate.com/src/sourcespotter/toolchain"
)

type Event struct {
	Version       toolchain.Version
	SourceURL     string // URL to source tar.gz to build
	BootstrapURL  string // URL to toolchain zip to use for bootstrapping
	BootstrapHash string // expected dirhash of bootstrap toolchain zip
	ZipUploadURL  string
	LogUploadURL  string
}
