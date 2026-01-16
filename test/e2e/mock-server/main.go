package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

type MockConfig struct {
	Delay      time.Duration
	StatusCode int
	Response   string
}

type Server struct {
	mu      sync.RWMutex
	config  MockConfig
	counter atomic.Int64
}

func NewServer() *Server {
	return &Server{
		config: MockConfig{
			Delay:      0,
			StatusCode: http.StatusOK,
			Response:   defaultMicroAppConfig(),
		},
	}
}

func defaultMicroAppConfig() string {
	return `{
  "kind": "MicroAppRuntimeConfiguration",
  "apiVersion": "ui.scality.com/v1alpha1",
  "metadata": {
    "kind": "shell",
    "name": "mock-component"
  },
  "spec": {
    "version": "1.0.0",
    "publicPath": "/mock/",
    "module": "./MicroApp",
    "views": {
      "main": {
        "path": "/mock",
        "label": {"en": "Mock"}
      }
    }
  }
}`
}

func (s *Server) handleMicroAppConfig(w http.ResponseWriter, r *http.Request) {
	s.counter.Add(1)
	log.Printf("Request #%d: %s %s", s.counter.Load(), r.Method, r.URL.Path)

	s.mu.RLock()
	config := s.config
	s.mu.RUnlock()

	if config.Delay > 0 {
		time.Sleep(config.Delay)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(config.StatusCode)
	w.Write([]byte(config.Response))
}

func (s *Server) handleCounter(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	count := s.counter.Load()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int64{"count": count})
}

func (s *Server) handleReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.counter.Store(0)
	s.mu.Lock()
	s.config = MockConfig{
		Delay:      0,
		StatusCode: http.StatusOK,
		Response:   defaultMicroAppConfig(),
	}
	s.mu.Unlock()

	log.Println("Server state reset")
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status": "reset"}`))
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Delay      int    `json:"delay"`
		StatusCode int    `json:"statusCode"`
		Response   string `json:"response"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	if req.Delay > 0 {
		s.config.Delay = time.Duration(req.Delay) * time.Millisecond
	}
	if req.StatusCode > 0 {
		s.config.StatusCode = req.StatusCode
	}
	if req.Response != "" {
		s.config.Response = req.Response
	}
	s.mu.Unlock()

	log.Printf("Config updated: delay=%dms, statusCode=%d", req.Delay, req.StatusCode)
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status": "updated"}`))
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status": "healthy"}`))
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "80"
	}

	server := NewServer()

	http.HandleFunc("/.well-known/micro-app-configuration", server.handleMicroAppConfig)
	http.HandleFunc("/_/counter", server.handleCounter)
	http.HandleFunc("/_/reset", server.handleReset)
	http.HandleFunc("/_/config", server.handleConfig)
	http.HandleFunc("/healthz", server.handleHealth)

	addr := fmt.Sprintf(":%s", port)
	log.Printf("Mock server starting on %s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
