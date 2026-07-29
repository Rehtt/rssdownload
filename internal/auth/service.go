package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/Rehtt/rssdownload/internal/store"
)

var ErrInvalidCredentials = errors.New("invalid username or password")

const sessionLifetime = 7 * 24 * time.Hour

type Service struct {
	store *store.Store
	now   func() time.Time
}

var dummyPasswordHash = func() string {
	hash, _ := HashPassword("rssdownload timing placeholder")
	return hash
}()

type LoginResult struct {
	Token     string
	CSRFToken string
	Session   store.Session
}

func New(store *store.Store) *Service {
	return &Service{
		store: store,
		now:   func() time.Time { return time.Now().UTC() },
	}
}

func (s *Service) Bootstrap(ctx context.Context, username, password string) error {
	count, err := s.store.AdminCount(ctx)
	if err != nil {
		return err
	}
	if count != 0 {
		return nil
	}
	if password == "" {
		return errors.New(
			"no administrator exists; set RSSDOWNLOAD_ADMIN_PASSWORD for the first start",
		)
	}
	passwordHash, err := HashPassword(password)
	if err != nil {
		return fmt.Errorf("hash bootstrap password: %w", err)
	}
	if err := s.store.CreateAdmin(ctx, username, passwordHash); err != nil {
		return err
	}
	return nil
}

func (s *Service) ResetPassword(ctx context.Context, username, password string) error {
	if password == "" {
		return errors.New("RSSDOWNLOAD_ADMIN_PASSWORD must be set")
	}
	passwordHash, err := HashPassword(password)
	if err != nil {
		return err
	}
	return s.store.UpdateAdminPassword(ctx, username, passwordHash)
}

func (s *Service) Login(ctx context.Context, username, password string) (LoginResult, error) {
	admin, err := s.store.AdminByUsername(ctx, username)
	if err != nil {
		// Keep a missing user reasonably close to the normal Argon2 cost.
		_ = VerifyPassword(dummyPasswordHash, password)
		return LoginResult{}, ErrInvalidCredentials
	}
	if !VerifyPassword(admin.PasswordHash, password) {
		return LoginResult{}, ErrInvalidCredentials
	}
	token, err := randomToken(32)
	if err != nil {
		return LoginResult{}, err
	}
	csrf, err := randomToken(32)
	if err != nil {
		return LoginResult{}, err
	}
	expires := s.now().Add(sessionLifetime)
	tokenHash := hashToken(token)
	if err := s.store.CreateSession(ctx, admin.ID, tokenHash, csrf, expires); err != nil {
		return LoginResult{}, err
	}
	session, err := s.store.SessionByTokenHash(ctx, tokenHash)
	if err != nil {
		return LoginResult{}, err
	}
	return LoginResult{Token: token, CSRFToken: csrf, Session: session}, nil
}

func (s *Service) Authenticate(ctx context.Context, token string) (store.Session, error) {
	if token == "" {
		return store.Session{}, ErrInvalidCredentials
	}
	session, err := s.store.SessionByTokenHash(ctx, hashToken(token))
	if errors.Is(err, store.ErrNotFound) {
		return store.Session{}, ErrInvalidCredentials
	}
	return session, err
}

func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return s.store.DeleteSession(ctx, hashToken(token))
}

func (s *Service) ChangePassword(
	ctx context.Context,
	session store.Session,
	currentPassword, newPassword string,
) error {
	admin, err := s.store.AdminByUsername(ctx, session.Username)
	if err != nil {
		return err
	}
	if !VerifyPassword(admin.PasswordHash, currentPassword) {
		return ErrInvalidCredentials
	}
	passwordHash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}
	return s.store.ChangeAdminPassword(
		ctx,
		session.Username,
		passwordHash,
		session.AdminID,
		session.ID,
	)
}

func randomToken(size int) (string, error) {
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("generate random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}
