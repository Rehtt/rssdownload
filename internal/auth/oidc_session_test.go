package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Rehtt/rssdownload/internal/database"
	"github.com/Rehtt/rssdownload/internal/oidctest"
	"github.com/Rehtt/rssdownload/internal/store"
)

func TestOIDCSessionUsesExistingAdminAndPasswordReset(t *testing.T) {
	ctx := context.Background()
	db, _, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close(db) })
	dataStore := store.New(db)
	s := New(dataStore)
	if err := s.Bootstrap(ctx, "existing-admin", "password123"); err != nil {
		t.Fatal(err)
	}
	p := oidctest.New(t)
	oidc := NewOIDC(p.Config())
	q, binding := startOIDC(t, oidc)
	if err := oidc.Complete(ctx, q.Get("state"), binding, q.Get("code")); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	s.now = func() time.Time { return now }
	login, err := s.LoginOIDC(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if login.Session.Username != "existing-admin" || login.Session.AdminID != 1 || !login.Session.ExpiresAt.Equal(now.Add(7*24*time.Hour)) {
		t.Fatalf("OIDC session = %#v", login.Session)
	}
	if got, err := s.Authenticate(ctx, login.Token); err != nil || got.CSRFToken != login.CSRFToken || got.CSRFToken == "" {
		t.Fatalf("session lookup = %#v, %v", got, err)
	}
	if count, err := dataStore.AdminCount(ctx); err != nil || count != 1 {
		t.Fatalf("admin count = %d, %v", count, err)
	}
	if err := s.ResetPassword(ctx, "existing-admin", "new-password123"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(ctx, login.Token); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("session after reset = %v", err)
	}
	if _, err := s.Login(ctx, "existing-admin", "new-password123"); err != nil {
		t.Fatalf("password login after reset: %v", err)
	}
}
