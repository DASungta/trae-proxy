package proxy

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/zhangyc/trae-proxy/internal/config"
)

type Server struct {
	Config     *config.Config
	HTTPClient *http.Client
	TLSConfig  *tls.Config
}

func NewServer(cfg *config.Config) *Server {
	return &Server{
		Config: cfg,
		HTTPClient: &http.Client{
			Timeout: 600 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
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
			HandleModels(s.Config)(w, r)
		case r.Method == "POST" && norm == "v1/chat/completions":
			HandleChatCompletions(s.Config, s.HTTPClient)(w, r)
		default:
			HandleForward(s.Config, s.HTTPClient)(w, r)
		}
	})
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	srv := &http.Server{
		Addr:    s.Config.Listen,
		Handler: s.Handler(),
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
		srv.Shutdown(context.Background())
	}()

	fmt.Printf("[trae-proxy] listening on %s → %s\n", s.Config.Listen, s.Config.Upstream)

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
