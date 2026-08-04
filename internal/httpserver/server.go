package httpserver

import (
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/pmstowski/newsletter/internal/config"
	"github.com/pmstowski/newsletter/internal/mailer"
	"github.com/pmstowski/newsletter/internal/ratelimit"
	"github.com/pmstowski/newsletter/internal/store"
)

type Dependencies struct {
	Store  store.Store
	Sender mailer.Sender
	Config config.Config
	Logger *slog.Logger
	Clock  func() time.Time
}

type server struct {
	store             store.Store
	sender            mailer.Sender
	cfg               config.Config
	logger            *slog.Logger
	clock             func() time.Time
	limiter           *ratelimit.Limiter
	trustProxyHeaders bool
}

func New(deps Dependencies) http.Handler {
	if deps.Logger == nil {
		deps.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if deps.Clock == nil {
		deps.Clock = time.Now
	}
	s := &server{
		store:             deps.Store,
		sender:            deps.Sender,
		cfg:               deps.Config,
		logger:            deps.Logger,
		clock:             deps.Clock,
		limiter:           ratelimit.New(deps.Config.RateLimitPerMinute, deps.Clock),
		trustProxyHeaders: deps.Config.TrustProxyHeaders,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /subscribe", s.handleSubscribe)
	mux.HandleFunc("GET /confirm", s.handleConfirm)
	mux.HandleFunc("POST /unsubscribe", s.handleUnsubscribe)
	mux.HandleFunc("GET /unsubscribe/confirm", s.handleUnsubscribeConfirm)
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	if deps.Config.AssetsDir != "" {
		mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServer(http.Dir(deps.Config.AssetsDir))))
	}
	if deps.Config.PublicDir != "" {
		mux.Handle("GET /", http.FileServer(http.Dir(deps.Config.PublicDir)))
	}
	return mux
}
