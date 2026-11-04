// Command goserve serves a directory over HTTP with Go's net/http
// (W17-S05): a Go user program as a server on Burrow, which exercises
// epoll, non-blocking accept and goroutines over inet sockets. It logs one
// line per request ("goserve: GET /path 200") so a test can follow it, and
// never writes to the directory it serves.
//
// Usage: goserve [-addr :8081] [-dir /home]
package main

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
)

// status records the code a handler wrote, for the log line.
type status struct {
	http.ResponseWriter
	code int
}

func (s *status) WriteHeader(code int) {
	s.code = code
	s.ResponseWriter.WriteHeader(code)
}

func logged(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		st := &status{ResponseWriter: w, code: http.StatusOK}
		h.ServeHTTP(st, r)
		fmt.Printf("goserve: %s %s %d\n", r.Method, r.URL.Path, st.code)
	})
}

func main() {
	addr := flag.String("addr", ":8081", "address to listen on")
	dir := flag.String("dir", "/home", "directory to serve")
	flag.Parse()
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "goserve: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("goserve: serving %s on %s\n", *dir, ln.Addr())
	if err := http.Serve(ln, logged(http.FileServer(http.Dir(*dir)))); err != nil {
		fmt.Fprintf(os.Stderr, "goserve: %v\n", err)
		os.Exit(1)
	}
}
