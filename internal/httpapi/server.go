package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Rehtt/rssdownload/internal/app"
	"github.com/Rehtt/rssdownload/internal/aria2"
	"github.com/Rehtt/rssdownload/internal/auth"
	"github.com/Rehtt/rssdownload/internal/domain"
	"github.com/Rehtt/rssdownload/internal/feed"
	proxyconfig "github.com/Rehtt/rssdownload/internal/proxy"
	"github.com/Rehtt/rssdownload/internal/store"
	"github.com/Rehtt/rssdownload/internal/webui"
)

const (
	sessionCookie = "rssdownload_session"
	maxJSONBody   = 1 << 20
)

type Server struct {
	store          *store.Store
	auth           *auth.Service
	oidc           *auth.OIDCService
	app            *app.Service
	logger         *slog.Logger
	cookieSecure   bool
	trustedProxies []netip.Prefix
	handler        http.Handler
	limiter        *loginLimiter
}

type contextKey string

type Option func(*Server)

func WithOIDC(service *auth.OIDCService) Option {
	return func(s *Server) { s.oidc = service }
}

const (
	sessionContextKey contextKey = "session"
	tokenContextKey   contextKey = "session-token"
)

func New(
	store *store.Store,
	authService *auth.Service,
	appService *app.Service,
	logger *slog.Logger,
	cookieSecure bool,
	trustedProxies []netip.Prefix,
	options ...Option,
) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	server := &Server{
		store:          store,
		auth:           authService,
		app:            appService,
		logger:         logger,
		cookieSecure:   cookieSecure,
		trustedProxies: trustedProxies,
		limiter:        newLoginLimiter(),
	}
	for _, option := range options {
		option(server)
	}
	server.handler = server.routes()
	return server
}

func (s *Server) Handler() http.Handler {
	return s.handler
}

func (s *Server) routes() http.Handler {
	root := http.NewServeMux()
	api := http.NewServeMux()

	api.HandleFunc("GET /api/v1/health", s.health)
	api.HandleFunc("POST /api/v1/auth/login", s.login)
	api.HandleFunc("GET /api/v1/auth/methods", s.authMethods)
	api.HandleFunc("GET /api/v1/auth/oidc/login", s.oidcLogin)
	api.HandleFunc("GET /api/v1/auth/oidc/callback", s.oidcCallback)

	protected := http.NewServeMux()
	protected.HandleFunc("GET /api/v1/auth/session", s.session)
	protected.HandleFunc("POST /api/v1/auth/logout", s.logout)
	protected.HandleFunc("PUT /api/v1/auth/password", s.changePassword)
	protected.HandleFunc("GET /api/v1/dashboard", s.dashboard)
	protected.HandleFunc("GET /api/v1/settings", s.getSettings)
	protected.HandleFunc("PUT /api/v1/settings", s.updateSettings)
	protected.HandleFunc("POST /api/v1/settings/test-aria2", s.testAria2)
	protected.HandleFunc("GET /api/v1/subscriptions", s.listSubscriptions)
	protected.HandleFunc("POST /api/v1/subscriptions", s.createSubscription)
	protected.HandleFunc("GET /api/v1/subscriptions/{id}", s.getSubscription)
	protected.HandleFunc("PATCH /api/v1/subscriptions/{id}", s.updateSubscription)
	protected.HandleFunc("DELETE /api/v1/subscriptions/{id}", s.archiveSubscription)
	protected.HandleFunc("POST /api/v1/subscriptions/{id}/restore", s.restoreSubscription)
	protected.HandleFunc("POST /api/v1/subscriptions/{id}/sync", s.syncSubscription)
	protected.HandleFunc("GET /api/v1/items", s.listItems)
	protected.HandleFunc("GET /api/v1/items/{id}", s.getItem)
	protected.HandleFunc("POST /api/v1/items/{id}/download", s.downloadItem)
	protected.HandleFunc("POST /api/v1/downloads/{id}/retry", s.retryDownload)
	protected.HandleFunc("POST /api/v1/downloads/{id}/pause", s.pauseDownload)
	protected.HandleFunc("POST /api/v1/downloads/{id}/resume", s.resumeDownload)

	api.Handle("/api/v1/", s.authenticate(protected))
	root.Handle("/api/v1/", api)
	root.Handle("/", webui.Handler())
	return s.securityHeaders(root)
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if !s.validSameOrigin(r) {
		writeError(w, http.StatusForbidden, "invalid_origin", "请求来源不受信任")
		return
	}
	ip := s.clientIP(r)
	if !s.limiter.Allow(ip) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "登录尝试过于频繁，请稍后再试")
		return
	}
	var request struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	result, err := s.auth.Login(r.Context(), strings.TrimSpace(request.Username), request.Password)
	if err != nil {
		s.limiter.Failed(ip)
		if errors.Is(err, auth.ErrInvalidCredentials) {
			writeError(w, http.StatusUnauthorized, "invalid_credentials", "用户名或密码错误")
			return
		}
		s.internalError(w, "login", err)
		return
	}
	s.limiter.Success(ip)
	s.setSessionCookie(w, result.Token)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{
		"username":  result.Session.Username,
		"csrfToken": result.CSRFToken,
	})
}

func (s *Server) session(w http.ResponseWriter, r *http.Request) {
	session := requestSession(r)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{
		"username":  session.Username,
		"csrfToken": session.CSRFToken,
	})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	token, _ := r.Context().Value(tokenContextKey).(string)
	if err := s.auth.Logout(r.Context(), token); err != nil {
		s.internalError(w, "logout", err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.cookieSecure,
		SameSite: http.SameSiteStrictMode,
	})
	w.Header().Set("Clear-Site-Data", `"cache", "cookies", "storage"`)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	var request struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	err := s.auth.ChangePassword(
		r.Context(),
		requestSession(r),
		request.CurrentPassword,
		request.NewPassword,
	)
	if errors.Is(err, auth.ErrInvalidCredentials) {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "当前密码错误")
		return
	}
	if err != nil {
		if errors.Is(err, auth.ErrWeakPassword) {
			writeError(w, http.StatusBadRequest, "weak_password", "新密码至少需要 8 个字符")
			return
		}
		s.internalError(w, "change password", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	result, err := s.store.Dashboard(r.Context())
	if err != nil {
		s.internalError(w, "dashboard", err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

type settingsRequest struct {
	Aria2URL            string   `json:"aria2Url"`
	Aria2Secret         string   `json:"aria2Secret"`
	ClearAria2Secret    bool     `json:"clearAria2Secret"`
	SeedTimeMinutes     *float64 `json:"seedTimeMinutes"`
	SeedRatio           *float64 `json:"seedRatio"`
	ProxyURL            string   `json:"proxyUrl"`
	ProxyUsername       string   `json:"proxyUsername"`
	ProxyPassword       string   `json:"proxyPassword"`
	ClearProxyPassword  bool     `json:"clearProxyPassword"`
	DefaultDownloadDir  string   `json:"defaultDownloadDir"`
	PollIntervalMinutes int      `json:"pollIntervalMinutes"`
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.store.Settings(r.Context())
	if err != nil {
		s.internalError(w, "get settings", err)
		return
	}
	writeJSON(w, http.StatusOK, publicSettings(settings))
}

func (s *Server) updateSettings(w http.ResponseWriter, r *http.Request) {
	var request settingsRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	current, err := s.store.Settings(r.Context())
	if err != nil {
		s.internalError(w, "get settings", err)
		return
	}
	if request.ClearAria2Secret {
		request.Aria2Secret = ""
	} else if request.Aria2Secret == "" || request.Aria2Secret == "••••" {
		request.Aria2Secret = current.Aria2Secret
	}
	request.ProxyURL = strings.TrimSpace(request.ProxyURL)
	request.ProxyUsername = strings.TrimSpace(request.ProxyUsername)
	if request.ProxyURL == "" {
		request.ProxyUsername = ""
		request.ProxyPassword = ""
	} else if request.ClearProxyPassword {
		request.ProxyPassword = ""
	} else if request.ProxyPassword == "" || request.ProxyPassword == "••••" {
		request.ProxyPassword = current.ProxyPassword
	}
	settings := domain.Settings{
		Aria2URL:            strings.TrimSpace(request.Aria2URL),
		Aria2Secret:         request.Aria2Secret,
		SeedTimeMinutes:     request.SeedTimeMinutes,
		SeedRatio:           request.SeedRatio,
		ProxyURL:            request.ProxyURL,
		ProxyUsername:       request.ProxyUsername,
		ProxyPassword:       request.ProxyPassword,
		DefaultDownloadDir:  strings.TrimSpace(request.DefaultDownloadDir),
		PollIntervalMinutes: request.PollIntervalMinutes,
	}
	if _, err := aria2.New(settings.Aria2URL, settings.Aria2Secret); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_aria2_url", err.Error())
		return
	}
	if err := (proxyconfig.Config{
		URL:      settings.ProxyURL,
		Username: settings.ProxyUsername,
		Password: settings.ProxyPassword,
	}).Validate(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_proxy", err.Error())
		return
	}
	updated, err := s.store.UpdateSettings(r.Context(), settings)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_settings", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, publicSettings(updated))
}

func (s *Server) testAria2(w http.ResponseWriter, r *http.Request) {
	var request settingsRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	current, err := s.store.Settings(r.Context())
	if err != nil {
		s.internalError(w, "get settings", err)
		return
	}
	if request.Aria2URL == "" {
		request.Aria2URL = current.Aria2URL
	}
	if request.ClearAria2Secret {
		request.Aria2Secret = ""
	} else if request.Aria2Secret == "" || request.Aria2Secret == "••••" {
		request.Aria2Secret = current.Aria2Secret
	}
	version, err := s.app.TestAria2(r.Context(), domain.Settings{
		Aria2URL:    strings.TrimSpace(request.Aria2URL),
		Aria2Secret: request.Aria2Secret,
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, "aria2_unavailable", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, version)
}

func (s *Server) listSubscriptions(w http.ResponseWriter, r *http.Request) {
	includeArchived := r.URL.Query().Get("includeArchived") == "true"
	result, err := s.store.ListSubscriptions(r.Context(), includeArchived)
	if err != nil {
		s.internalError(w, "list subscriptions", err)
		return
	}
	for index := range result {
		result[index].URL = feed.RedactURL(result[index].URL)
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) createSubscription(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Name             string `json:"name"`
		URL              string `json:"url"`
		DownloadDir      string `json:"downloadDir"`
		DownloadExisting bool   `json:"downloadExisting"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	request.URL = strings.TrimSpace(request.URL)
	if err := feed.ValidateMikanURL(request.URL); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_subscription_url", err.Error())
		return
	}
	result, err := s.store.CreateSubscription(
		r.Context(),
		request.Name,
		request.URL,
		request.DownloadDir,
		request.DownloadExisting,
	)
	if err != nil {
		s.internalError(w, "create subscription", err)
		return
	}
	s.app.TriggerSync(result.ID)
	result.URL = feed.RedactURL(result.URL)
	writeJSON(w, http.StatusCreated, result)
}

func (s *Server) getSubscription(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	result, err := s.store.Subscription(r.Context(), id)
	if err != nil {
		s.storeError(w, err)
		return
	}
	result.URL = feed.RedactURL(result.URL)
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) updateSubscription(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var request struct {
		Name        *string `json:"name"`
		URL         *string `json:"url"`
		Enabled     *bool   `json:"enabled"`
		DownloadDir *string `json:"downloadDir"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if request.URL != nil {
		value := strings.TrimSpace(*request.URL)
		if value == "" || strings.Contains(value, "••••") || strings.Contains(value, "token=REDACTED") {
			request.URL = nil
		} else {
			if err := feed.ValidateMikanURL(value); err != nil {
				writeError(w, http.StatusBadRequest, "invalid_subscription_url", err.Error())
				return
			}
			request.URL = &value
		}
	}
	result, err := s.store.UpdateSubscription(r.Context(), id, store.SubscriptionPatch{
		Name:        request.Name,
		URL:         request.URL,
		Enabled:     request.Enabled,
		DownloadDir: request.DownloadDir,
	})
	if err != nil {
		s.storeError(w, err)
		return
	}
	if request.URL != nil || (request.Enabled != nil && *request.Enabled) {
		s.app.TriggerSync(id)
	}
	result.URL = feed.RedactURL(result.URL)
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) archiveSubscription(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.store.ArchiveSubscription(r.Context(), id); err != nil {
		s.storeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) restoreSubscription(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.store.RestoreSubscription(r.Context(), id); err != nil {
		s.storeError(w, err)
		return
	}
	s.app.TriggerSync(id)
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) syncSubscription(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	subscription, err := s.store.Subscription(r.Context(), id)
	if err != nil {
		s.storeError(w, err)
		return
	}
	if subscription.ArchivedAt != nil {
		writeError(w, http.StatusConflict, "subscription_archived", "归档订阅不能同步")
		return
	}
	if !s.app.TriggerSync(id) {
		writeError(w, http.StatusConflict, "sync_in_progress", "订阅正在同步")
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) listItems(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	result, err := s.store.ListItems(r.Context(), store.ItemFilter{
		Page:           queryInt(query, "page", 1),
		PageSize:       queryInt(query, "pageSize", 20),
		SubscriptionID: int64(queryInt(query, "subscriptionId", 0)),
		Recognition:    query.Get("recognition"),
		DownloadStatus: query.Get("downloadStatus"),
		Search:         query.Get("search"),
	})
	if err != nil {
		s.internalError(w, "list items", err)
		return
	}
	for index := range result.Items {
		result.Items[index].EnclosureURL = redactTorrentURL(result.Items[index].EnclosureURL)
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) getItem(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	result, err := s.store.Item(r.Context(), id)
	if err != nil {
		s.storeError(w, err)
		return
	}
	result.EnclosureURL = redactTorrentURL(result.EnclosureURL)
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) downloadItem(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	result, err := s.store.QueueItemDownload(r.Context(), id, false)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.storeError(w, err)
		} else {
			writeError(w, http.StatusConflict, "item_not_downloadable", err.Error())
		}
		return
	}
	writeJSON(w, http.StatusAccepted, result)
}

func (s *Server) retryDownload(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	result, err := s.store.ResetDownload(r.Context(), id)
	if err != nil {
		s.storeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, result)
}

func (s *Server) pauseDownload(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.app.PauseDownload(r.Context(), id); err != nil {
		s.actionError(w, "pause_download", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) resumeDownload(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.app.ResumeDownload(r.Context(), id); err != nil {
		s.actionError(w, "resume_download", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookie)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "请先登录")
			return
		}
		session, err := s.auth.Authenticate(r.Context(), cookie.Value)
		if err != nil {
			http.SetCookie(w, &http.Cookie{
				Name:     sessionCookie,
				Value:    "",
				Path:     "/",
				MaxAge:   -1,
				HttpOnly: true,
				Secure:   s.cookieSecure,
				SameSite: http.SameSiteStrictMode,
			})
			writeError(w, http.StatusUnauthorized, "unauthorized", "登录已失效")
			return
		}
		if isMutation(r.Method) {
			if !s.validSameOrigin(r) {
				writeError(w, http.StatusForbidden, "invalid_origin", "请求来源不受信任")
				return
			}
			if r.Header.Get("X-CSRF-Token") != session.CSRFToken {
				writeError(w, http.StatusForbidden, "invalid_csrf", "CSRF 校验失败")
				return
			}
		}
		ctx := context.WithValue(r.Context(), sessionContextKey, session)
		ctx = context.WithValue(ctx, tokenContextKey, cookie.Value)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set(
			"Content-Security-Policy",
			"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data:; connect-src 'self'; font-src 'self' data:; frame-ancestors 'none'",
		)
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) storeError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "记录不存在")
		return
	}
	s.internalError(w, "store operation", err)
}

func (s *Server) actionError(w http.ResponseWriter, operation string, err error) {
	if errors.Is(err, store.ErrNotFound) {
		s.storeError(w, err)
		return
	}
	writeError(w, http.StatusBadGateway, operation+"_failed", err.Error())
}

func (s *Server) internalError(w http.ResponseWriter, operation string, err error) {
	s.logger.Error(operation, "error", err)
	writeError(w, http.StatusInternalServerError, "internal_error", "服务器内部错误")
}

func publicSettings(settings domain.Settings) map[string]any {
	secret := ""
	if settings.Aria2Secret != "" {
		secret = "••••"
	}
	proxyPassword := ""
	if settings.ProxyPassword != "" {
		proxyPassword = "••••"
	}
	return map[string]any{
		"aria2Url":            settings.Aria2URL,
		"aria2Secret":         secret,
		"hasAria2Secret":      settings.Aria2Secret != "",
		"seedTimeMinutes":     settings.SeedTimeMinutes,
		"seedRatio":           settings.SeedRatio,
		"proxyUrl":            settings.ProxyURL,
		"proxyUsername":       settings.ProxyUsername,
		"proxyPassword":       proxyPassword,
		"hasProxyPassword":    settings.ProxyPassword != "",
		"defaultDownloadDir":  settings.DefaultDownloadDir,
		"pollIntervalMinutes": settings.PollIntervalMinutes,
		"updatedAt":           settings.UpdatedAt,
	}
}

func requestSession(r *http.Request) store.Session {
	session, _ := r.Context().Value(sessionContextKey).(store.Session)
	return session
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusBadRequest, "invalid_id", "无效的记录 ID")
		return 0, false
	}
	return id, true
}

func queryInt(values url.Values, name string, fallback int) int {
	raw := values.Get(name)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	if contentType := r.Header.Get("Content-Type"); contentType != "" &&
		!strings.HasPrefix(strings.ToLower(contentType), "application/json") {
		return errors.New("Content-Type 必须是 application/json")
	}
	reader := http.MaxBytesReader(w, r.Body, maxJSONBody)
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("无效的 JSON：%w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("请求只能包含一个 JSON 对象")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}

func (s *Server) validSameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		// Non-browser clients and same-origin form-free requests often omit Origin.
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := r.Host
	if forwarded := r.Header.Get("X-Forwarded-Host"); forwarded != "" && s.fromTrustedProxy(r) {
		host = strings.TrimSpace(strings.Split(forwarded, ",")[0])
	}
	return strings.EqualFold(parsed.Host, host)
}

// clientIP returns the address used to rate-limit login attempts. Forwarding
// headers are only honored when the direct peer is a configured trusted
// proxy; otherwise anyone could spoof X-Forwarded-For to dodge the limiter.
func (s *Server) clientIP(r *http.Request) string {
	peer, ok := peerAddr(r)
	if !ok {
		return r.RemoteAddr
	}
	if !s.isTrustedProxy(peer) {
		return peer.String()
	}
	entries := forwardedForEntries(r)
	// Walk right to left: the first hop that is not one of our proxies is
	// the client address as seen by the nearest trusted proxy.
	for index := len(entries) - 1; index >= 0; index-- {
		addr, ok := parseForwardedAddr(entries[index])
		if !ok {
			break
		}
		if !s.isTrustedProxy(addr) {
			return addr.String()
		}
	}
	return peer.String()
}

func (s *Server) fromTrustedProxy(r *http.Request) bool {
	peer, ok := peerAddr(r)
	return ok && s.isTrustedProxy(peer)
}

func (s *Server) isTrustedProxy(addr netip.Addr) bool {
	for _, prefix := range s.trustedProxies {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func peerAddr(r *http.Request) (netip.Addr, bool) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}

func forwardedForEntries(r *http.Request) []string {
	var entries []string
	for _, header := range r.Header.Values("X-Forwarded-For") {
		for _, entry := range strings.Split(header, ",") {
			if entry = strings.TrimSpace(entry); entry != "" {
				entries = append(entries, entry)
			}
		}
	}
	return entries
}

func parseForwardedAddr(entry string) (netip.Addr, bool) {
	if addr, err := netip.ParseAddr(entry); err == nil {
		return addr.Unmap(), true
	}
	if addrPort, err := netip.ParseAddrPort(entry); err == nil {
		return addrPort.Addr().Unmap(), true
	}
	return netip.Addr{}, false
}

func isMutation(method string) bool {
	return method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions
}

func redactTorrentURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

type loginLimiter struct {
	mu        sync.Mutex
	attempts  map[string][]time.Time
	blocked   map[string]time.Time
	lastSweep time.Time
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{
		attempts: make(map[string][]time.Time),
		blocked:  make(map[string]time.Time),
	}
}

func (l *loginLimiter) Allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	l.sweep(now)
	if until, exists := l.blocked[ip]; exists {
		if now.Before(until) {
			return false
		}
		delete(l.blocked, ip)
	}
	cutoff := now.Add(-10 * time.Minute)
	recent := l.attempts[ip][:0]
	for _, attempt := range l.attempts[ip] {
		if attempt.After(cutoff) {
			recent = append(recent, attempt)
		}
	}
	if len(recent) == 0 {
		delete(l.attempts, ip)
	} else {
		l.attempts[ip] = recent
	}
	return len(recent) < 5
}

func (l *loginLimiter) Failed(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	l.attempts[ip] = append(l.attempts[ip], now)
	if len(l.attempts[ip]) >= 5 {
		l.blocked[ip] = now.Add(15 * time.Minute)
	}
}

// sweep drops stale per-IP state so unauthenticated scans cannot grow the
// maps without bound. The caller must hold l.mu.
func (l *loginLimiter) sweep(now time.Time) {
	if now.Sub(l.lastSweep) < 10*time.Minute {
		return
	}
	l.lastSweep = now
	cutoff := now.Add(-10 * time.Minute)
	for ip, attempts := range l.attempts {
		stale := true
		for _, attempt := range attempts {
			if attempt.After(cutoff) {
				stale = false
				break
			}
		}
		if stale {
			delete(l.attempts, ip)
		}
	}
	for ip, until := range l.blocked {
		if !now.Before(until) {
			delete(l.blocked, ip)
		}
	}
}

func (l *loginLimiter) Success(ip string) {
	l.mu.Lock()
	delete(l.attempts, ip)
	delete(l.blocked, ip)
	l.mu.Unlock()
}
