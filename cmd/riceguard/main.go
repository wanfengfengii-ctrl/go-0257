// Command riceguard runs the RiceGuard inspection backend, persists the task
// aggregate to SQLite WAL and serves the browser inspection console.
package main

import (
	"flag"
	"log"
	"net/http"
	"os"

	"riceguard/api"
	"riceguard/catalog"
	"riceguard/measure"
	"riceguard/pathogen"
	"riceguard/store"
)

func main() {
	addr := flag.String("addr", envOr("RICE_ADDR", ":8080"), "listen address")
	dbPath := flag.String("db", envOr("RICE_DB", "riceguard.db"), "sqlite database path")
	staticDir := flag.String("static", envOr("RICE_STATIC_DIR", "frontend/dist"), "frontend static directory")
	flag.Parse()

	cat, roles := catalog.Seed()
	st, err := store.OpenSQLite(*dbPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()

	amp := pathogen.NewStaticAmplifier()
	meter := measure.NewScriptedMeter()
	svc := api.NewService(cat, roles, st, amp, meter)
	srv := api.NewServer(svc, *staticDir)

	log.Printf("RiceGuard listening on %s (db=%s static=%s)", *addr, *dbPath, *staticDir)
	if err := http.ListenAndServe(*addr, srv.Handler()); err != nil {
		log.Fatalf("server: %v", err)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
