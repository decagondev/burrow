// Command gofetch fetches a URL with Go's net/http and prints the body
// (W17-S06): DNS through /etc/resolv.conf, TCP over Burrow's sockets, and
// for https a TLS handshake in Go's own crypto, checked against the
// Mozilla roots built into the binary (golang.org/x/crypto's
// x509roots/fallback: Burrow has no CA store). -k skips the check (a
// test server's self-signed certificate); -o writes the body to a file;
// -stats reports the transfer on stderr. Never writes anywhere else.
//
// Usage: gofetch [-k] [-o file] [-stats] URL
package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	_ "golang.org/x/crypto/x509roots/fallback" // roots for a system without them
)

func main() {
	insecure := flag.Bool("k", false, "do not check the server's certificate")
	out := flag.String("o", "", "write the body to this file instead of stdout")
	stats := flag.Bool("stats", false, "report size, time and rate on stderr")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: gofetch [-k] [-o file] [-stats] URL")
		os.Exit(2)
	}
	client := &http.Client{Timeout: 60 * time.Second}
	if *insecure {
		client.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	}
	if err := fetch(client, flag.Arg(0), *out, *stats); err != nil {
		fmt.Fprintf(os.Stderr, "gofetch: %v\n", err)
		os.Exit(1)
	}
}

func fetch(client *http.Client, url, out string, stats bool) error {
	start := time.Now()
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	w := io.Writer(os.Stdout)
	if out != "" {
		f, err := os.Create(out)
		if err != nil {
			return err
		}
		defer f.Close()
		w = f
	}
	n, err := io.Copy(w, resp.Body)
	if err != nil {
		return err
	}
	took := time.Since(start)
	if stats {
		fmt.Fprintf(os.Stderr, "gofetch: %s, %d bytes in %d ms (%.1f MB/s)\n",
			resp.Status, n, took.Milliseconds(), float64(n)/1e6/took.Seconds())
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s", resp.Status)
	}
	return nil
}
