package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

var Version = "1.3.0"
const RepoName = "TarsRAGFlow"

type Client struct {
	Endpoint string
	APIKey   string
	Client   *http.Client
}

func NewClient(endpoint, apiKey string) *Client {
	if endpoint == "" {
		endpoint = "https://api.example.com"
	}
	return &Client{
		Endpoint: strings.TrimRight(endpoint, "/"),
		APIKey:   apiKey,
		Client:   &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) req(ctx context.Context, method, path string, body interface{}) (map[string]interface{}, error) {
	var bodyR io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		bodyR = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Endpoint+path, bodyR)
	if err != nil { return nil, err }
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.Client.Do(req)
	if err != nil { return nil, err }
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(b))
	}
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return map[string]interface{}{"raw": "non-JSON response"}, nil
	}
	return result, nil
}

func loadConfig() map[string]string {
	return map[string]string{
		"endpoint": os.Getenv("TARS_MCP_ENDPOINT"),
		"api_key":  os.Getenv("TARS_MCP_API_KEY"),
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
	client := NewClient(cfg["endpoint"], cfg["api_key"])
	log.Printf("[%s] starting v%s endpoint=%s", RepoName, Version, cfg["endpoint"])
	mode := os.Getenv("TARS_MCP_MODE")
	if mode == "" { mode = "stdio" }
	srv := &http.Server{Addr: ":8080", ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second}
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { withClient(client, healthHandler)(w, r) })
	http.HandleFunc("/tools", func(w http.ResponseWriter, r *http.Request) { withClient(client, toolsHandler)(w, r) })
	http.HandleFunc("/invoke", func(w http.ResponseWriter, r *http.Request) { withClient(client, invokeHandler)(w, r) })
	http.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"requests": reqCounter.Load(),
			"errors":   errCounter.Load(),
			"limited":  limitCounter.Load(),
			"version":  Version,
		})
	})
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Printf("[%s] shutting down", RepoName)
		_ = srv.Close()
	}()
	if mode == "stdio" {
		runStdioServer(client)
		return
	}
	log.Printf("[%s] HTTP listening on %s", RepoName, srv.Addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}

var (
	reqCounter   atomic.Int64
	errCounter   atomic.Int64
	limitCounter atomic.Int64
)

func genRequestID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

type responseRecorder struct {
	http.ResponseWriter
	status int
	size   int
}

func (rr *responseRecorder) WriteHeader(code int) {
	rr.status = code
	rr.ResponseWriter.WriteHeader(code)
}

func (rr *responseRecorder) Write(b []byte) (int, error) {
	n, err := rr.ResponseWriter.Write(b)
	rr.size += n
	return n, err
}

func loggingMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		reqID := r.Header.Get("X-Request-ID")
		if reqID == "" { reqID = genRequestID() }
		w.Header().Set("X-Request-ID", reqID)
		rr := &responseRecorder{ResponseWriter: w, status: http.StatusOK}
		next(rr, r)
		log.Printf("[%s] reqID=%s %s %s -> %d (%d bytes, %v)", RepoName, reqID, r.Method, r.URL.Path, rr.status, rr.size, time.Since(start))
		reqCounter.Add(1)
		if rr.status >= 400 { errCounter.Add(1) }
	}
}

type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*tokenBucket
	limit   int
	window  time.Duration
}

type tokenBucket struct {
	tokens int
	last   time.Time
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{buckets: make(map[string]*tokenBucket), limit: limit, window: window}
}

func (rl *rateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	b, ok := rl.buckets[ip]
	if !ok || now.Sub(b.last) > rl.window {
		rl.buckets[ip] = &tokenBucket{tokens: rl.limit - 1, last: now}
		return true
	}
	if b.tokens > 0 {
		b.tokens--
		return true
	}
	return false
}

var globalLimiter = newRateLimiter(60, time.Minute)

func rateLimitMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := r.RemoteAddr
		if idx := strings.Index(ip, ":"); idx != -1 { ip = ip[:idx] }
		if !globalLimiter.allow(ip) {
			limitCounter.Add(1)
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "请求过于频繁，请稍后再试"})
			return
		}
		next(w, r)
	}
}

func withClient(client *Client, fn func(http.ResponseWriter, *http.Request, *Client)) http.HandlerFunc {
	return rateLimitMiddleware(loggingMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions { w.WriteHeader(http.StatusNoContent); return }
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		fn(w, r, client)
	})))
}

func healthHandler(w http.ResponseWriter, r *http.Request, client *Client) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	up := map[string]interface{}{"healthy": false}
	if _, err := client.req(ctx, "GET", "/health", nil); err == nil { up["healthy"] = true }
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok", "version": Version, "name": RepoName, "upstream": up,
		"time": time.Now().UTC().Format(time.RFC3339),
	})
}

func toolsHandler(w http.ResponseWriter, r *http.Request, client *Client) {
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"tools": []map[string]interface{}{
		{
			"name":        "ragflow_datasets",
			"description": "List knowledge base datasets",
			"inputSchema": map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
				"required":   []interface{}{},
			},
		},
		{
			"name":        "ragflow_chat",
			"description": "Chat with a RAGFlow knowledge base",
			"inputSchema": map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{"dataset_id": map[string]interface{}{"type": "string", "description": "Parameter dataset_id"}, "question": map[string]interface{}{"type": "string", "description": "Parameter question"}},
				"required":   []interface{}{"dataset_id", "question"},
			},
		},
		},
	})
}

func invokeHandler(w http.ResponseWriter, r *http.Request, client *Client) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "只接受 POST 请求"})
		return
	}
	var req struct { Tool string `json:"tool"`; Params map[string]interface{} `json:"params"` }
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": fmt.Sprintf("解析失败：%v", err)})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	switch req.Tool {
	case "ragflow_datasets":
		result, err := client.req(ctx, "GET", "/api/datasets", nil)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": fmt.Sprintf("调用失败：%v", err)})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"result": result})
	case "ragflow_chat":
		if req.Params["dataset_id"] == nil { w.WriteHeader(http.StatusBadRequest); _ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "缺少参数：dataset_id"}); return }
		if req.Params["question"] == nil { w.WriteHeader(http.StatusBadRequest); _ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "缺少参数：question"}); return }
		body := map[string]interface{}{"dataset_id": req.Params["dataset_id"], "question": req.Params["question"]}
		result, err := client.req(ctx, "POST", "/api/chats", body)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": fmt.Sprintf("调用失败：%v", err)})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"result": result})
	default:
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": fmt.Sprintf("不认识工具：%s", req.Tool)})
	}
}

func runStdioServer(client *Client) {
	log.Printf("[%s] stdio mode", RepoName)
	dec := json.NewDecoder(os.Stdin)
	enc := json.NewEncoder(os.Stdout)
	for {
		var msg map[string]interface{}
		if err := dec.Decode(&msg); err != nil { return }
		id := msg["id"]
		method, _ := msg["method"].(string)
		var result map[string]interface{}
		switch method {
		case "tools/list":
			result = map[string]interface{}{"tools": []map[string]interface{}{{"name": "ragflow_datasets", "description": "List knowledge base datasets"}, {"name": "ragflow_chat", "description": "Chat with a RAGFlow knowledge base"}}}
		case "tools/call":
			params, _ := msg["params"].(map[string]interface{})
			toolName, _ := params["name"].(string)
			args, _ := params["arguments"].(map[string]interface{})
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			result = handleStdioTool(ctx, client, toolName, args)
			cancel()
		default:
			result = map[string]interface{}{"message": fmt.Sprintf("%s v%s ready", RepoName, Version)}
		}
		_ = enc.Encode(map[string]interface{}{"jsonrpc": "2.0", "id": id, "result": result})
	}
}

func handleStdioTool(ctx context.Context, client *Client, toolName string, args map[string]interface{}) map[string]interface{} {
	switch toolName {
	case "ragflow_datasets":
		result, err := client.req(ctx, "GET", "/api/datasets", nil)
		if err != nil {
			return map[string]interface{}{"content": []map[string]string{{"type": "text", "text": fmt.Sprintf("调用失败：%v", err)}}}
		}
		data, _ := json.Marshal(result)
		return map[string]interface{}{"content": []map[string]string{{"type": "text", "text": string(data)}}}
	case "ragflow_chat":
		if args["dataset_id"] == nil { return map[string]interface{}{"content": []map[string]string{{"type": "text", "text": "缺少参数：dataset_id"}}} }
		if args["question"] == nil { return map[string]interface{}{"content": []map[string]string{{"type": "text", "text": "缺少参数：question"}}} }
		result, err := client.req(ctx, "POST", "/api/chats", map[string]interface{}{"dataset_id": args["dataset_id"], "question": args["question"]})
		if err != nil {
			return map[string]interface{}{"content": []map[string]string{{"type": "text", "text": fmt.Sprintf("调用失败：%v", err)}}}
		}
		data, _ := json.Marshal(result)
		return map[string]interface{}{"content": []map[string]string{{"type": "text", "text": string(data)}}}
	default:
		return map[string]interface{}{"content": []map[string]string{{"type": "text", "text": fmt.Sprintf("不认识工具：%s", toolName)}}}
	}
}
