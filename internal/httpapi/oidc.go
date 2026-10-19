package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/Rehtt/rssdownload/internal/auth"
)

const oidcCookie = "rssdownload_oidc"

func (s *Server) authMethods(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"password": true, "oidc": s.oidc.Enabled()})
}

func (s *Server) setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/", HttpOnly: true,
		Secure: s.cookieSecure,
		// Lax allows the session on the redirect back from the identity provider.
		// Mutations still require same-origin checks and the session's CSRF token.
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) setOIDCCookie(w http.ResponseWriter, value string, clear bool) {
	cookie := &http.Cookie{
		Name: oidcCookie, Value: value, Path: "/api/v1/auth/oidc",
		HttpOnly: true, Secure: s.cookieSecure, SameSite: http.SameSiteLaxMode,
		MaxAge:  int(auth.OIDCTransactionLifetime.Seconds()),
		Expires: time.Now().Add(auth.OIDCTransactionLifetime),
	}
	if clear {
		cookie.MaxAge = -1
		cookie.Expires = time.Unix(1, 0)
	}
	http.SetCookie(w, cookie)
}

func (s *Server) oidcFailure(w http.ResponseWriter, r *http.Request, code string) {
	// Never log provider errors: OAuth errors can contain codes, tokens or secrets.
	s.logger.Warn("OIDC login failed", "code", code)
	http.Redirect(w, r, "/login?oidc_error="+code, http.StatusSeeOther)
}

func (s *Server) oidcLogin(w http.ResponseWriter, r *http.Request) {
	if !s.oidc.Enabled() {
		s.oidcFailure(w, r, "oidc_disabled")
		return
	}
	ip := s.clientIP(r)
	if !s.limiter.Allow(ip) {
		s.oidcFailure(w, r, "oidc_rate_limited")
		return
	}
	// Count starts as attempts, so abandoning a flow cannot bypass the IP limit.
	s.limiter.Failed(ip)
	location, binding, err := s.oidc.Begin(r.Context())
	if err != nil {
		s.oidcFailure(w, r, "oidc_unavailable")
		return
	}
	s.setOIDCCookie(w, binding, false)
	http.Redirect(w, r, location, http.StatusFound)
}

func (s *Server) oidcCallback(w http.ResponseWriter, r *http.Request) {
	s.setOIDCCookie(w, "", true)
	if !s.oidc.Enabled() {
		s.oidcFailure(w, r, "oidc_disabled")
		return
	}
	binding := ""
	if cookie, err := r.Cookie(oidcCookie); err == nil {
		binding = cookie.Value
	}
	query := r.URL.Query()
	code := query.Get("code")
	if query.Has("error") {
		code = ""
	}
	err := s.oidc.Complete(r.Context(), query.Get("state"), binding, code)
	if err != nil {
		errorCode := "oidc_failed"
		switch {
		case errors.Is(err, auth.ErrOIDCState):
			errorCode = "oidc_invalid_state"
		case errors.Is(err, auth.ErrOIDCAccess):
			errorCode = "oidc_access_denied"
		case errors.Is(err, auth.ErrOIDCUnavailable):
			errorCode = "oidc_unavailable"
		}
		s.oidcFailure(w, r, errorCode)
		return
	}
	result, err := s.auth.LoginOIDC(r.Context())
	if err != nil {
		s.oidcFailure(w, r, "oidc_failed")
		return
	}
	s.limiter.Success(s.clientIP(r))
	s.setSessionCookie(w, result.Token)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
