package httpx

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"
)

type Server struct {
	name   string
	addr   string
	logger *slog.Logger
	mux    *http.ServeMux
	opts   Timeouts

	mu       sync.Mutex
	listener net.Listener
	server   *http.Server
}

type Timeouts struct {
	ReadTimeout       time.Duration
	ReadHeaderTimeout time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
}

func New(name, addr string, logger *slog.Logger, opts Timeouts) *Server {
	if addr == "" {
		addr = ":18080"
	}
	return &Server{
		name:   name,
		addr:   addr,
		logger: logger,
		mux:    http.NewServeMux(),
		opts:   normalizeTimeouts(opts),
	}
}

func (s *Server) Name() string {
	return s.name
}

func (s *Server) Mux() *http.ServeMux {
	return s.mux
}

func (s *Server) Start(_ context.Context) error {
	s.mu.Lock()
	if s.listener != nil {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}

	s.mu.Lock()
	s.listener = ln
	s.server = NewHTTPServer(s.mux, s.opts)
	server := s.server
	s.mu.Unlock()

	go func() {
		err := server.Serve(ln)
		if err != nil && !errors.Is(err, http.ErrServerClosed) && s.logger != nil {
			s.logger.Error("admin http serve failed", "error", err)
		}
	}()

	if s.logger != nil {
		s.logger.Info("admin http listening", "addr", ln.Addr().String())
	}
	return nil
}

func NewHTTPServer(handler http.Handler, opts Timeouts) *http.Server {
	opts = normalizeTimeouts(opts)
	return &http.Server{
		Handler:           handler,
		ReadTimeout:       opts.ReadTimeout,
		ReadHeaderTimeout: opts.ReadHeaderTimeout,
		WriteTimeout:      opts.WriteTimeout,
		IdleTimeout:       opts.IdleTimeout,
	}
}

func normalizeTimeouts(opts Timeouts) Timeouts {
	if opts.ReadTimeout <= 0 {
		opts.ReadTimeout = 10 * time.Second
	}
	if opts.ReadHeaderTimeout <= 0 {
		opts.ReadHeaderTimeout = 5 * time.Second
	}
	if opts.WriteTimeout <= 0 {
		opts.WriteTimeout = 15 * time.Second
	}
	if opts.IdleTimeout <= 0 {
		opts.IdleTimeout = 60 * time.Second
	}
	return opts
}

func (s *Server) Stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	server := s.server
	s.server = nil
	s.listener = nil
	s.mu.Unlock()
	if server == nil {
		return nil
	}
	if s.logger != nil {
		s.logger.Info("stopping admin http", "addr", s.addr)
	}
	return server.Shutdown(ctx)
}
