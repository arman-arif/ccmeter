package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"sync"
	"time"
)

//go:embed web
var webFS embed.FS

func serve(cfg Config, cfgPath string, pr *Pricer, addr string, every time.Duration) error {
	accts := make([]*Account, len(cfg.Accounts))
	for i, c := range cfg.Accounts {
		accts[i] = newAccount(c)
	}
	scan := func() {
		cutoff := time.Now().Add(-historyWindow)
		var wg sync.WaitGroup
		for _, a := range accts {
			wg.Add(1)
			go func(a *Account) { defer wg.Done(); a.Scan(cutoff) }(a)
		}
		wg.Wait()
	}
	log.Printf("scanning %d account(s)...", len(accts))
	scan()
	go func() {
		for range time.Tick(every) {
			scan()
		}
	}()

	mux := http.NewServeMux()
	sub, _ := fs.Sub(webFS, "web")
	mux.Handle("/", http.FileServer(http.FS(sub)))
	mux.HandleFunc("/api/summary", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(buildReport(accts, pr, time.Now()))
	})

	fmt.Printf("ccmeter dashboard on http://%s  (%d accounts, rescans every %s)\n", addr, len(accts), every)
	return http.ListenAndServe(addr, mux)
}
