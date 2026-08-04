package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/pmstowski/newsletter/internal/config"
	"github.com/pmstowski/newsletter/internal/httpserver"
	"github.com/pmstowski/newsletter/internal/mailer"
	"github.com/pmstowski/newsletter/internal/store"
	"github.com/pmstowski/newsletter/internal/worker"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("service stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger.Info("configuration loaded", "http_addr", cfg.HTTPAddr, "database", databaseSummary(cfg.DatabaseURL), "newsletter_dir", cfg.NewsletterDir)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		return err
	}

	smtpSender, err := mailer.NewSMTPSender(cfg)
	if err != nil {
		return err
	}

	handler := httpserver.New(httpserver.Dependencies{
		Store:  st,
		Sender: smtpSender,
		Config: cfg,
		Logger: logger,
		Clock:  time.Now,
	})
	server := newHTTPServer(cfg, loggingMiddleware(logger, handler))

	go worker.RunScanner(ctx, cfg.NewsletterScanInterval, func(ctx context.Context) error {
		return worker.ScanOnce(ctx, st, cfg.NewsletterDir, cfg.SMTPFromName, logger)
	}, logger)
	go worker.RunSender(ctx, time.Second, func(ctx context.Context) (bool, error) {
		return worker.SendOne(ctx, st, smtpSender, cfg.SendDelay, cfg.MaxSendAttempts, logger)
	}, logger)

	errCh := make(chan error, 1)
	go func() {
		logger.Info("http server starting", "addr", cfg.HTTPAddr)
		errCh <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return err
		}
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func loggingMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
		logger.Info("http request", "method", r.Method, "path", r.URL.Path, "remote_addr", r.RemoteAddr)
	})
}

func newHTTPServer(cfg config.Config, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

func databaseSummary(databaseURL string) string {
	if before, _, ok := strings.Cut(databaseURL, "://"); ok {
		return before + "://..."
	}
	return "unknown"
}
