// mocksubs is a minimal mock Center /api/subs endpoint for the k2r UAT
// multislot phase. It serves the current bytes of a JSON file on disk for
// any request, re-reading the file on every request — so the test script
// can simulate binding-manifest changes (or clearing them) by simply
// overwriting the file, no restart or admin API needed.
package main

import (
	"flag"
	"log/slog"
	"net/http"
	"os"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:8443", "HTTPS listen address")
	certPath := flag.String("cert", "", "TLS certificate path")
	keyPath := flag.String("key", "", "TLS key path")
	jsonPath := flag.String("json", "", "path to the JSON file served as the subs response body")
	flag.Parse()

	if *certPath == "" || *keyPath == "" || *jsonPath == "" {
		slog.Error("mocksubs: -cert, -key and -json are required")
		os.Exit(1)
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		data, err := os.ReadFile(*jsonPath)
		if err != nil {
			http.Error(w, "response body not found", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
	})

	slog.Info("mocksubs: listening", "addr", *listen, "json", *jsonPath)
	if err := http.ListenAndServeTLS(*listen, *certPath, *keyPath, nil); err != nil {
		slog.Error("mocksubs: serve failed", "err", err)
		os.Exit(1)
	}
}
