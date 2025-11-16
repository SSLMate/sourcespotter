package modcheck

import (
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
	"software.sslmate.com/src/sourcespotter"
	"src.agwa.name/go-dbutil"
)

type versionInfo struct {
	Version          string
	ExpectedSum      string
	ExpectedGoModSum string
	ObservedAt       time.Time
	Download         struct {
		Sum      string         `json:",omitzero"`
		GoModSum string         `json:",omitzero"`
		Error    string         `json:",omitzero"`
		Origin   map[string]any `json:",omitzero"`
	}
}

func Serve(w http.ResponseWriter, req *http.Request) {
	module := req.URL.Query().Get("module")
	if module == "" {
		http.Error(w, "missing module parameter", http.StatusBadRequest)
		return
	}

	var records []struct {
		Version      string    `sql:"version"`
		SourceSHA256 []byte    `sql:"source_sha256"`
		GomodSHA256  []byte    `sql:"gomod_sha256"`
		ObservedAt   time.Time `sql:"observed_at"`
	}
	if err := dbutil.QueryAll(req.Context(), sourcespotter.DB, &records, `SELECT version, source_sha256, gomod_sha256, observed_at FROM record WHERE module = $1`, module); err != nil {
		log.Printf("modcheck: query failed: %v", err)
		http.Error(w, "Internal Database Error", http.StatusInternalServerError)
		return
	}
	gopath, err := os.MkdirTemp("", "modcheck")
	if err != nil {
		log.Printf("modcheck: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	defer os.RemoveAll(gopath)

	rc := http.NewResponseController(w)
	rc.SetWriteDeadline(time.Now().Add(5 * time.Minute))

	w.Header().Set("Content-Type", "application/jsonl")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Expose-Headers", "Sourcespotter-Records")
	w.Header().Set("Sourcespotter-Records", strconv.Itoa(len(records)))
	w.WriteHeader(http.StatusOK)
	rc.Flush()

	var encoderMu sync.Mutex
	encoder := json.NewEncoder(w)

	g, ctx := errgroup.WithContext(req.Context())
	g.SetLimit(10)
	for _, record := range records {
		if ctx.Err() != nil {
			break
		}
		g.Go(func() error {
			info := versionInfo{
				Version:          record.Version,
				ExpectedSum:      "h1:" + base64.StdEncoding.EncodeToString(record.SourceSHA256),
				ExpectedGoModSum: "h1:" + base64.StdEncoding.EncodeToString(record.GomodSHA256),
				ObservedAt:       record.ObservedAt,
			}
			cmd := exec.CommandContext(ctx, "go", "mod", "download", "-modcacherw", "-json", "--", module+"@"+record.Version)
			cmd.Env = []string{
				"HOME=" + os.Getenv("HOME"),
				"USER=" + os.Getenv("USER"),
				"PATH=" + os.Getenv("PATH"),
				"LOGNAME=" + os.Getenv("LOGNAME"),
				"PWD=/",
				"GOPROXY=direct",
				"GOSUMDB=off",
				"GOPATH=" + gopath,
			}
			cmd.Dir = "/"
			out, execErr := cmd.Output()
			if err := json.Unmarshal(out, &info.Download); err != nil && execErr == nil {
				info.Download.Error = "error unmarshalling JSON from `go mod download`: " + err.Error()
			} else if execErr != nil && info.Download.Error == "" {
				info.Download.Error = "error execing `go mod download`: " + execErr.Error()
			}

			encoderMu.Lock()
			defer encoderMu.Unlock()
			if err := encoder.Encode(info); err != nil {
				return err
			}
			rc.Flush()
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		log.Printf("modcheck: %v", err)
	}
}
