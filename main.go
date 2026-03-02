package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"

	"rssbridge/internal/admin"
	"rssbridge/internal/scheduler"
	"rssbridge/internal/store"
)

func main() {
	portFlag := flag.String("port", "", "HTTP port (overrides DB setting and RSSBRIDGE_PORT env var)")
	dbPath := flag.String("db", "rssbridge.db", "Path to SQLite database file")
	flag.Parse()

	// Open store
	st, err := store.New(*dbPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()

	// Determine port: flag > env > DB setting
	port := ""
	if *portFlag != "" {
		port = *portFlag
	} else if env := os.Getenv("RSSBRIDGE_PORT"); env != "" {
		port = env
	} else {
		if p, err := st.GetSetting("port"); err == nil {
			port = p
		}
	}
	if port == "" {
		port = "7171"
	}

	// Start scheduler
	sc := scheduler.New(st)
	sc.Start()

	// Build admin handler
	templateDir := templateDirectory()
	h, err := admin.New(st, sc, templateDir)
	if err != nil {
		log.Fatalf("admin init: %v", err)
	}

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	addr := ":" + port
	fmt.Printf("rssBridge listening on http://localhost%s/admin\n", addr)
	fmt.Printf("RSS feed: http://localhost%s/rss\n", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

// templateDirectory returns the path to the templates/ directory relative to
// the binary's location, falling back to the source-adjacent path for `go run`.
func templateDirectory() string {
	// When built as a binary, templates live next to the binary.
	exe, err := os.Executable()
	if err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "templates")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}

	// During `go run`, use the caller's source file location.
	_, file, _, ok := runtime.Caller(0)
	if ok {
		candidate := filepath.Join(filepath.Dir(file), "templates")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}

	// Fallback: relative to working directory
	return "templates"
}
