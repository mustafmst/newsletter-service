package httpserver

import (
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/pmstowski/newsletter/internal/emailaddr"
	"github.com/pmstowski/newsletter/internal/mailer"
	"github.com/pmstowski/newsletter/internal/store"
	"github.com/pmstowski/newsletter/internal/token"
)

func (s *server) handleSubscribe(w http.ResponseWriter, r *http.Request) {
	if !s.allow(r) {
		http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	email, err := emailaddr.Normalize(r.FormValue("email"))
	if err != nil {
		http.Error(w, "invalid email", http.StatusBadRequest)
		return
	}
	now := s.clock()
	sub, err := s.store.UpsertPendingSubscriber(r.Context(), email, now)
	if err != nil {
		s.logger.Error("upsert pending subscriber failed", "error", err)
		http.Error(w, "subscribe failed", http.StatusInternalServerError)
		return
	}
	plain, hash, err := token.New()
	if err != nil {
		s.logger.Error("token generation failed", "error", err)
		http.Error(w, "subscribe failed", http.StatusInternalServerError)
		return
	}
	if _, err := s.store.CreateToken(r.Context(), sub.ID, store.TokenConfirmSubscribe, hash, now.Add(s.cfg.TokenTTL), now); err != nil {
		s.logger.Error("create subscription token failed", "error", err)
		http.Error(w, "subscribe failed", http.StatusInternalServerError)
		return
	}
	msg := mailer.NewConfirmationMessage(s.cfg.SMTPFromEmail, s.cfg.SMTPFromName, email, s.cfg.PublicBaseURL, plain)
	if err := s.sender.Send(r.Context(), msg); err != nil {
		s.logger.Error("send confirmation failed", "error", err)
		http.Error(w, "subscribe failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte("confirmation sent"))
}

func (s *server) handleConfirm(w http.ResponseWriter, r *http.Request) {
	plain := r.URL.Query().Get("token")
	if plain == "" {
		s.serveTokenError(w, r)
		return
	}
	if _, err := s.store.ActivateSubscriberByToken(r.Context(), token.Hash(plain), s.clock()); err != nil {
		s.logger.Warn("subscription token rejected", "error", err)
		s.serveTokenError(w, r)
		return
	}
	s.servePage(w, r, s.cfg.SubscribeSuccessPage, "subscription confirmed")
}

func (s *server) handleUnsubscribe(w http.ResponseWriter, r *http.Request) {
	if !s.allow(r) {
		http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	email, err := emailaddr.Normalize(r.FormValue("email"))
	if err != nil {
		http.Error(w, "invalid email", http.StatusBadRequest)
		return
	}
	now := s.clock()
	sub, ok, err := s.store.FindSubscriberByEmail(r.Context(), email)
	if err != nil {
		s.logger.Error("find subscriber failed", "error", err)
		http.Error(w, "unsubscribe failed", http.StatusInternalServerError)
		return
	}
	if ok {
		plain, hash, err := token.New()
		if err != nil {
			s.logger.Error("token generation failed", "error", err)
			http.Error(w, "unsubscribe failed", http.StatusInternalServerError)
			return
		}
		if _, err := s.store.CreateToken(r.Context(), sub.ID, store.TokenConfirmUnsubscribe, hash, now.Add(s.cfg.TokenTTL), now); err != nil {
			s.logger.Error("create unsubscribe token failed", "error", err)
			http.Error(w, "unsubscribe failed", http.StatusInternalServerError)
			return
		}
		msg := mailer.NewUnsubscribeMessage(s.cfg.SMTPFromEmail, s.cfg.SMTPFromName, email, s.cfg.PublicBaseURL, plain)
		if err := s.sender.Send(r.Context(), msg); err != nil {
			s.logger.Error("send unsubscribe confirmation failed", "error", err)
			http.Error(w, "unsubscribe failed", http.StatusInternalServerError)
			return
		}
	}
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte("if subscribed, an unsubscribe confirmation was sent"))
}

func (s *server) handleUnsubscribeConfirm(w http.ResponseWriter, r *http.Request) {
	plain := r.URL.Query().Get("token")
	if plain == "" {
		s.serveTokenError(w, r)
		return
	}
	if _, err := s.store.UnsubscribeSubscriberByToken(r.Context(), token.Hash(plain), s.clock()); err != nil {
		s.logger.Warn("unsubscribe token rejected", "error", err)
		s.serveTokenError(w, r)
		return
	}
	s.servePage(w, r, s.cfg.UnsubscribeSuccessPage, "unsubscribed")
}

func (s *server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Ping(r.Context()); err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (s *server) allow(r *http.Request) bool {
	return s.limiter.Allow(clientIP(r, s.trustProxyHeaders))
}

func clientIP(r *http.Request, trustProxyHeaders bool) string {
	if trustProxyHeaders {
		forwarded := r.Header.Get("X-Forwarded-For")
		if forwarded != "" {
			first, _, _ := strings.Cut(forwarded, ",")
			if trimmed := strings.TrimSpace(first); trimmed != "" {
				return trimmed
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func (s *server) serveTokenError(w http.ResponseWriter, r *http.Request) {
	s.servePage(w, r, s.cfg.TokenErrorPage, "token is invalid or expired")
}

func (s *server) servePage(w http.ResponseWriter, r *http.Request, name string, fallback string) {
	if s.cfg.PublicDir != "" && name != "" {
		path := filepath.Join(s.cfg.PublicDir, name)
		if _, err := os.Stat(path); err == nil {
			http.ServeFile(w, r, path)
			return
		}
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(fallback))
}
