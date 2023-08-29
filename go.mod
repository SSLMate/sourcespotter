module software.sslmate.com/src/sourcespotter

go 1.20

require (
	github.com/lib/pq v1.10.7
	src.agwa.name/go-dbutil v0.5.0
	src.agwa.name/go-listener v0.5.0
)

require (
	golang.org/x/crypto v0.12.0 // indirect
	golang.org/x/net v0.14.0 // indirect
	golang.org/x/sync v0.3.0 // indirect
	golang.org/x/text v0.12.0 // indirect
	software.sslmate.com/src/certspotter v0.16.0 // indirect
)

replace software.sslmate.com/src/certspotter => ../certspotter
