// Command webhookmirror is a tiny test-support HTTP server: it records
// every request it receives (method, path, headers, body) and exposes them
// back over a small JSON API, so integration tests can point formelay's
// webhook/discord channels at it and assert on exactly what was delivered,
// without depending on any real webhook/Discord-mocking service.
//
// Not part of the shipped product: .goreleaser.yml and the Dockerfile only
// ever build ./cmd/formelay. This binary exists solely to be run by
// docker-compose.test.yml (see `make test-integration`).
package main

import (
	"encoding/json"
	"flag"
	"io"
	"log"
	"net/http"
	"sync"
	"time"
)

// capturedRequest is one recorded request, in the shape returned by the
// GET /_captured API.
type capturedRequest struct {
	Method     string      `json:"method"`
	Path       string      `json:"path"`
	Headers    http.Header `json:"headers"`
	Body       string      `json:"body"`
	ReceivedAt time.Time   `json:"received_at"`
}

type recorder struct {
	mu       sync.Mutex
	captured []capturedRequest
}

func (r *recorder) record(req capturedRequest) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.captured = append(r.captured, req)
}

func (r *recorder) list() []capturedRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]capturedRequest, len(r.captured))
	copy(out, r.captured)
	return out
}

func (r *recorder) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.captured = nil
}

func (r *recorder) handle(w http.ResponseWriter, req *http.Request) {
	if req.URL.Path == "/_captured" {
		switch req.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(r.list())
		case http.MethodDelete:
			r.reset()
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
		return
	}

	body, _ := io.ReadAll(req.Body)
	r.record(capturedRequest{
		Method:     req.Method,
		Path:       req.URL.Path,
		Headers:    req.Header.Clone(),
		Body:       string(body),
		ReceivedAt: time.Now(),
	})
	w.WriteHeader(http.StatusOK)
}

func main() {
	addr := flag.String("addr", ":8090", "address to listen on")
	flag.Parse()

	rec := &recorder{}
	log.Printf("webhookmirror listening on %s", *addr)
	if err := http.ListenAndServe(*addr, http.HandlerFunc(rec.handle)); err != nil {
		log.Fatal(err)
	}
}
