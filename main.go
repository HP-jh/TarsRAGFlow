package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// Version is set via -ldflags at build time
var Version = "0.1.0"

// RepoName is the GitHub repo name (TarsXxx)
const RepoName = "TarsRAGFlow"

func loadConfig() map[string]string {
	return map[string]string{
		"endpoint":  os.Getenv("TARS_MCP_ENDPOINT"),
		"api_key":   os.Getenv("TARS_MCP_API_KEY"),
		"log_level": "info",
	}
}

func main() {
	version := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *version {
		fmt.Printf("%s v%s\n", RepoName, Version)
		return
	}

	cfg := loadConfig()
	log.Printf("[%s] starting v%s endpoint=%s", RepoName, Version, cfg["endpoint"])

	mode := os.Getenv("TARS_MCP_MODE")
	if mode == "" {
		mode = "stdio"
	}

	srv := &http.Server{
		Addr:         ":8080",
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	http.HandleFunc("/health", healthHandler)
	http.HandleFunc("/tools", toolsHandler)
	http.HandleFunc("/invoke", invokeHandler)

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Printf("[%s] shutting down", RepoName)
		_ = srv.Close()
	}()

	if mode == "stdio" {
		runStdioServer()
		return
	}

	log.Printf("[%s] HTTP listening on %s", RepoName, srv.Addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":  "ok",
		"version": Version,
		"name":    RepoName,
		"time":    time.Now().UTC().Format(time.RFC3339),
	})
}

func toolsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"tools": []map[string]string{
			{"name": "ragflow_list_models", "description": "List available upstream models"},
			{"name": "ragflow_invoke", "description": "Invoke upstream with prompt + params"},
			{"name": "ragflow_health", "description": "Health check of upstream connection"},
			{"name": "ragflow_status", "description": "Current TSG integration status"},
		},
	})
}

func invokeHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotImplemented)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error":   "not_implemented",
		"message": "MCP invoke handler is scaffold in v0.1.0 MVP. Real upstream integration is added in v0.2.0+.",
		"version": Version,
	})
}

func runStdioServer() {
	log.Printf("[%s] running in stdio mode (MCP protocol over stdin/stdout)", RepoName)
	dec := json.NewDecoder(os.Stdin)
	enc := json.NewEncoder(os.Stdout)
	for {
		var msg map[string]any
		if err := dec.Decode(&msg); err != nil {
			return
		}
		_ = enc.Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      msg["id"],
			"result": map[string]any{
				"message": fmt.Sprintf("%s v%%s MCP stdio scaffold ready", RepoName, Version),
			},
		})
	}
}
