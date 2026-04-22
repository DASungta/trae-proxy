package proxy

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/zhangyc/trae-proxy/internal/config"
	"github.com/zhangyc/trae-proxy/internal/logging"
)

type Server struct {
	Config       *config.Config
	Logger       *logging.Logger
	HTTPClient   *http.Client
	BypassClient *http.Client // uses public DNS (1.1.1.1), ignores /etc/hosts
	TLSConfig    *tls.Config

	clientCache map[string]*http.Client // key: scheme://host
	clientMu    sync.RWMutex
}

type serverErrorLogWriter struct {
	logger *logging.Logger
}

func (w *serverErrorLogWriter) Write(p []byte) (int, error) {
	trimmed := strings.TrimSpace(string(p))
	if trimmed == "" {
		return len(p), nil
	}
	w.logger.Warn("server error", "msg", trimmed)
	return len(p), nil
}

func buildHTTPClient() *http.Client {
	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	return &http.Client{
		Transport: &http.Transport{
			DialContext:           dialer.DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 60 * time.Second,
			IdleConnTimeout:       90 * time.Second,
			MaxIdleConns:          20,
			MaxIdleConnsPerHost:   20,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func extractHostKey(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return rawURL
	}
	return parsed.Scheme + "://" + parsed.Host
}

func NewServer(cfg *config.Config, logger *logging.Logger) *Server {
	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{Timeout: 5 * time.Second}
			return d.DialContext(ctx, "udp", "1.1.1.1:53")
		},
	}
	bypassDialer := &net.Dialer{
		Resolver:  resolver,
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	bypassClient := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return bypassDialer.DialContext(ctx, network, addr)
			},
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
			IdleConnTimeout:       90 * time.Second,
		},
	}

	s := &Server{
		Config:       cfg,
		Logger:       logger,
		BypassClient: bypassClient,
		clientCache:  make(map[string]*http.Client),
	}
	if upstream := cfg.DefaultUpstream(); upstream != nil {
		s.HTTPClient = s.clientFor(upstream)
	} else {
		s.HTTPClient = buildHTTPClient()
	}
	for _, upstream := range cfg.Upstreams {
		s.clientFor(upstream)
	}
	return s
}

func (s *Server) clientFor(u *config.Upstream) *http.Client {
	if u == nil {
		if s.HTTPClient != nil {
			return s.HTTPClient
		}
		return buildHTTPClient()
	}

	key := extractHostKey(u.URL)
	s.clientMu.RLock()
	if c, ok := s.clientCache[key]; ok {
		s.clientMu.RUnlock()
		return c
	}
	s.clientMu.RUnlock()

	s.clientMu.Lock()
	defer s.clientMu.Unlock()
	if c, ok := s.clientCache[key]; ok {
		return c
	}
	c := buildHTTPClient()
	s.clientCache[key] = c
	return c
}

func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := stripAPIPrefix(r.URL.Path)
		norm := strings.TrimLeft(path, "/")
		if idx := strings.Index(norm, "?"); idx >= 0 {
			norm = norm[:idx]
		}

		switch {
		case r.Method == "GET" && norm == "v1/models":
			HandleModels(s)(w, r)
		case r.Method == "POST" && norm == "v1/chat/completions":
			HandleChatCompletions(s)(w, r)
		default:
			HandleForward(s)(w, r)
		}
	})
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	srv := &http.Server{
		Addr:              s.Config.Listen,
		Handler:           s.Handler(),
		ErrorLog:          log.New(&serverErrorLogWriter{logger: s.Logger}, "", 0),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	if s.TLSConfig != nil {
		srv.TLSConfig = s.TLSConfig
	}

	ln, err := net.Listen("tcp", s.Config.Listen)
	if err != nil {
		return fmt.Errorf("listen %s: %w", s.Config.Listen, err)
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer shutdownCancel()
		srv.Shutdown(shutdownCtx)
	}()

	defaultUpstream := s.Config.DefaultUpstream()
	defaultURL := ""
	if defaultUpstream != nil {
		defaultURL = defaultUpstream.URL
	}
	s.Logger.Info("listening",
		"addr", s.Config.Listen,
		"default_upstream", defaultURL,
		"upstream_count", len(s.Config.Upstreams),
	)

	if s.TLSConfig != nil {
		ln = tls.NewListener(ln, s.TLSConfig)
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			return err
		}
		return nil
	}
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}
