// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

// Package server implements gputop's service (agent) mode: a read-only HTTP
// API serving snapshots, history and Prometheus metrics.
//
// Security model:
//   - Default listen address is loopback only.
//   - Listening on a non-loopback address requires TLS and bearer-token
//     authentication; gputop refuses to start otherwise.
//   - Tokens are compared in constant time against a SHA-256 digest; the
//     plaintext token never needs to exist on the server (token_sha256).
//   - Optional mutual TLS (client_ca_file).
//   - The API is read-only (GET/HEAD) and never serves process command
//     lines; process names/users can be withheld with expose_processes.
package server

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gputop/gputop/internal/config"
	"github.com/gputop/gputop/internal/history"
	"github.com/gputop/gputop/internal/model"
	"github.com/gputop/gputop/internal/paths"
)

// Source provides data to serve.
type Source interface {
	Latest() *model.Snapshot
	History() history.Reader
}

// Options configure the server.
type Options struct {
	Server     config.Server
	Prometheus config.Prometheus
	Source     Source
	Version    string
	Log        *slog.Logger
}

// Server is the HTTP API server.
type Server struct {
	o         Options
	log       *slog.Logger
	tokenHash []byte
	tlsConf   *tls.Config
	http      *http.Server
}

// New validates the security configuration and builds the server.
func New(o Options) (*Server, error) {
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	s := &Server{o: o, log: o.Log}
	cfg := o.Server
	host, _, err := net.SplitHostPort(cfg.Listen)
	if err != nil {
		return nil, fmt.Errorf("server.listen: %w", err)
	}

	switch {
	case cfg.TokenSHA256 != "":
		h, err := hex.DecodeString(cfg.TokenSHA256)
		if err != nil || len(h) != sha256.Size {
			return nil, errors.New("server.token_sha256 must be a hex SHA-256 digest")
		}
		s.tokenHash = h
	case cfg.TokenFile != "":
		tok, err := ReadToken(paths.Expand(cfg.TokenFile), s.log)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256([]byte(tok))
		s.tokenHash = sum[:]
	}

	if cfg.TLSCertFile != "" {
		cert, err := tls.LoadX509KeyPair(paths.Expand(cfg.TLSCertFile), paths.Expand(cfg.TLSKeyFile))
		if err != nil {
			return nil, fmt.Errorf("loading TLS certificate: %w", err)
		}
		s.tlsConf = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
		if cfg.ClientCAFile != "" {
			pem, err := os.ReadFile(paths.Expand(cfg.ClientCAFile))
			if err != nil {
				return nil, fmt.Errorf("reading client CA: %w", err)
			}
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(pem) {
				return nil, errors.New("client CA file contains no certificates")
			}
			s.tlsConf.ClientCAs = pool
			s.tlsConf.ClientAuth = tls.RequireAndVerifyClientCert
		}
	}

	if !isLoopbackListen(host) {
		if s.tlsConf == nil || s.tokenHash == nil {
			return nil, fmt.Errorf("refusing to listen on non-loopback address %q without TLS and token authentication "+
				"(set server.tls_cert_file/tls_key_file and server.token_file or token_sha256, "+
				"or keep the default 127.0.0.1 and use an SSH tunnel)", cfg.Listen)
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.Handle("/api/v1/snapshot", s.auth(http.HandlerFunc(s.handleSnapshot)))
	mux.Handle("/api/v1/summary", s.auth(http.HandlerFunc(s.handleSummary)))
	mux.Handle("/api/v1/history", s.auth(http.HandlerFunc(s.handleHistory)))
	mux.Handle("/api/v1/events", s.auth(http.HandlerFunc(s.handleEvents)))
	if o.Prometheus.Enabled {
		mux.Handle(o.Prometheus.Path, s.auth(http.HandlerFunc(s.handleMetrics)))
	}
	s.http = &http.Server{
		Addr: cfg.Listen, Handler: s.middleware(mux), TLSConfig: s.tlsConf,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 30 * time.Second,
		IdleTimeout: 60 * time.Second, MaxHeaderBytes: 64 << 10,
		ErrorLog: slog.NewLogLogger(s.log.Handler(), slog.LevelWarn),
	}
	return s, nil
}

func isLoopbackListen(host string) bool {
	return host != "" && config.IsLoopbackHost(host)
}

// ReadToken reads a token file and warns about loose permissions.
func ReadToken(path string, log *slog.Logger) (string, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("token file: %w", err)
	}
	if fi.Mode().Perm()&0o077 != 0 && log != nil {
		log.Warn("token file is readable by group/others; chmod 600 recommended", "file", path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("token file: %w", err)
	}
	tok := strings.TrimSpace(string(b))
	if len(tok) < 16 {
		return "", errors.New("token must be at least 16 characters (generate one with gputop --gen-token)")
	}
	return tok, nil
}

// Handler exposes the HTTP handler (tests).
func (s *Server) Handler() http.Handler { return s.http.Handler }

// Run serves until ctx is cancelled.
func (s *Server) Run(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.o.Server.Listen)
	if err != nil {
		return err
	}
	scheme := "http"
	if s.tlsConf != nil {
		ln = tls.NewListener(ln, s.tlsConf)
		scheme = "https"
	}
	s.log.Info("gputop service listening", "address", scheme+"://"+ln.Addr().String(), "auth", s.tokenHash != nil, "mtls", s.tlsConf != nil && s.tlsConf.ClientCAs != nil)
	errCh := make(chan error, 1)
	go func() { errCh <- s.http.Serve(ln) }()
	select {
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.http.Shutdown(shutdown)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.tokenHash != nil {
			h := r.Header.Get("Authorization")
			tok, ok := strings.CutPrefix(h, "Bearer ")
			sum := sha256.Sum256([]byte(tok))
			if !ok || subtle.ConstantTimeCompare(sum[:], s.tokenHash) != 1 {
				w.Header().Set("WWW-Authenticate", `Bearer realm="gputop"`)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	if err := enc.Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Redact removes process details that must not leave the host.
func Redact(s *model.Snapshot, exposeProcesses bool) *model.Snapshot {
	c := *s
	c.Processes = make([]model.Process, len(s.Processes))
	for i, p := range s.Processes {
		p.Command = "" // never served remotely: may contain secrets
		if !exposeProcesses {
			p.Name, p.User = "", ""
		}
		c.Processes[i] = p
	}
	return &c
}

func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, Redact(s.o.Source.Latest(), s.o.Server.ExposeProcesses))
}

func (s *Server) handleSummary(w http.ResponseWriter, r *http.Request) {
	snap := s.o.Source.Latest()
	writeJSON(w, &model.Snapshot{
		Schema: snap.Schema, Seq: snap.Seq, Time: snap.Time, Ready: snap.Ready, Node: snap.Node,
		Providers: snap.Providers, Fleet: snap.Fleet, Alerts: snap.Alerts, History: snap.History,
	})
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	snap := s.o.Source.Latest()
	since, err := parseSince(r.URL.Query().Get("since"), time.Hour)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var out []model.Event
	for _, e := range snap.Events {
		if !e.Time.Before(since) {
			out = append(out, e)
		}
	}
	writeJSON(w, map[string]any{"events": out})
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	reader := s.o.Source.History()
	if reader == nil {
		http.Error(w, "history is disabled on this agent", http.StatusNotFound)
		return
	}
	q := r.URL.Query()
	since, err := parseSince(q.Get("since"), 30*time.Minute)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	hq := history.Query{Since: since, MaxPoints: 2000}
	if v := q.Get("max_points"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			http.Error(w, "invalid max_points", http.StatusBadRequest)
			return
		}
		hq.MaxPoints = min(n, 10000)
	}
	if v := q.Get("keys"); v != "" {
		hq.Keys = strings.Split(v, ",")
	}
	if v := q.Get("metrics"); v != "" {
		for _, k := range strings.Split(v, ",") {
			m, ok := history.ByKey(k)
			if !ok {
				http.Error(w, "unknown metric "+k, http.StatusBadRequest)
				return
			}
			hq.Metrics = append(hq.Metrics, m)
		}
	}
	res, err := reader.Query(r.Context(), hq)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, res)
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	if err := WritePrometheus(w, s.o.Source.Latest(), s.o.Version); err != nil {
		s.log.Warn("writing metrics", "err", err)
	}
}

func parseSince(v string, def time.Duration) (time.Time, error) {
	if v == "" {
		return time.Now().Add(-def), nil
	}
	if d, err := time.ParseDuration(v); err == nil {
		if d <= 0 || d > 31*24*time.Hour {
			return time.Time{}, errors.New("since must be within (0, 744h]")
		}
		return time.Now().Add(-d), nil
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return time.Time{}, errors.New("since must be a duration (15m) or RFC 3339 time")
	}
	return t, nil
}
