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
	"github.com/zhangyc/trae-proxy/internal/logging"
)


type Server struct {
	Config       *config.Config
	Logger       *logging.Logger
	HTTPClient   *http.Client
	BypassClient *http.Client // uses public DNS (1.1.1.1), ignores /etc/hosts
	TLSConfig    *tls.Config
}

func NewServer(cfg *config.Config, logger *logging.Logger) *Server {
	// BypassClient: custom resolver via 1.1.1.1 to bypass the /etc/hosts hijack.
	// PreferGo forces the pure-Go DNS resolver, enabling the custom Dial.
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
		Timeout: 30 * time.Second, // BypassClient 只用于 /v1/models，不涉及流式，保留整体超时
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return bypassDialer.DialContext(ctx, network, addr)
			},
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
			IdleConnTimeout:       90 * time.Second,
		},
	}

	// HTTPClient 用于所有上游代理请求（包括 SSE 流式响应）。
	// 不设 http.Client.Timeout — 该字段覆盖整个请求生命周期含响应体读取，
	// 会强制切断仍在传输的 SSE 流。改用 Transport 分阶段超时：
	//   - DialContext/TLSHandshakeTimeout 覆盖连接建立阶段
	//   - ResponseHeaderTimeout 防止上游不响应 header（挂起连接）
	//   - IdleConnTimeout 回收空闲连接
	// 客户端断开由 context 传播 + 写错误检测共同兜底。
	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	return &Server{
		Config: cfg,
		Logger: logger,
		HTTPClient: &http.Client{
			Transport: &http.Transport{
				DialContext:           dialer.DialContext,
				TLSHandshakeTimeout:   10 * time.Second,
				ResponseHeaderTimeout: 60 * time.Second,
				IdleConnTimeout:       90 * time.Second,
				MaxIdleConns:          20,
				MaxIdleConnsPerHost:   20, // 所有流量到同一上游 host，默认值 2 会导致频繁建连
			},
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		BypassClient: bypassClient,
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
			HandleModels(s)(w, r)
		case r.Method == "POST" && norm == "v1/chat/completions":
			if s.Config.UpstreamProtocol == "openai" {
				HandleForward(s.Config, s.Logger, s.HTTPClient)(w, r)
			} else {
				HandleChatCompletions(s.Config, s.Logger, s.HTTPClient)(w, r)
			}
		default:
			HandleForward(s.Config, s.Logger, s.HTTPClient)(w, r)
		}
	})
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	srv := &http.Server{
		Addr:    s.Config.Listen,
		Handler: s.Handler(),
		// ReadHeaderTimeout 防止 slowloris 攻击（慢速发送请求头）
		ReadHeaderTimeout: 10 * time.Second,
		// ReadTimeout 覆盖读取完整请求（header + body）的最大时长
		ReadTimeout: 60 * time.Second,
		// IdleTimeout 限制 keep-alive 空闲连接的保留时长
		IdleTimeout: 120 * time.Second,
		// WriteTimeout 不设：SSE 流式响应需要长期写入，由写错误检测 + context 取消兜底
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
		// 30s 内等待在途请求完成；超时后强制关闭，避免 SSE 流导致进程永远挂起
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer shutdownCancel()
		srv.Shutdown(shutdownCtx)
	}()

	s.Logger.Info("listening",
		"addr", s.Config.Listen,
		"upstream", s.Config.Upstream,
		"protocol", s.Config.UpstreamProtocol,
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
