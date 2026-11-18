package service

import (
	"context"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"sync"
	"time"
	"unicode/utf8"

	"qimeng.local/next/server/internal/model"
)

const passwordIterations = 600000

type passwordLogin struct{ salt, key []byte }
type loginBudget struct {
	mu       sync.Mutex
	start    time.Time
	attempts int
	slots    chan struct{}
}
type SessionInput struct {
	Token    string `json:"token"`
	Password string `json:"password"`
	Label    string `json:"label"`
}

func preparePassword(password string) (*passwordLogin, error) {
	if password == "" {
		return nil, nil
	}
	if !utf8.ValidString(password) || utf8.RuneCountInString(password) < 12 || len(password) > 1024 {
		return nil, errors.New("configured login password must be 12 or more characters and at most 1024 bytes")
	}
	salt := make([]byte, 32)
	if _, e := rand.Read(salt); e != nil {
		return nil, e
	}
	key, e := pbkdf2.Key(sha256.New, password, salt, passwordIterations, 32)
	if e != nil {
		return nil, e
	}
	return &passwordLogin{salt: salt, key: key}, nil
}

// The deployment password is never passed to Authenticate, persisted in a
// receipt, returned to a client, or accepted as a Bearer token. Each successful
// login mints its own device session through the normal CreateSession use case.
func (s *Service) AuthenticateLogin(ctx context.Context, in SessionInput) (model.Principal, error) {
	if (in.Token == "") == (in.Password == "") {
		return model.Principal{}, Fail(400, "login_credential_required", "请输入密码或可信设备凭据，两者只能提供一项。")
	}
	value := in.Token
	if in.Password != "" {
		value = in.Password
	}
	if len(value) > 1024 {
		return model.Principal{}, Fail(400, "invalid_login", "登录输入过长。")
	}
	s.loginLimit.mu.Lock()
	now := time.Now()
	if now.Sub(s.loginLimit.start) >= time.Minute {
		s.loginLimit.start = now
		s.loginLimit.attempts = 0
	}
	if s.loginLimit.attempts >= 20 {
		s.loginLimit.mu.Unlock()
		return model.Principal{}, Fail(429, "login_rate_limited", "登录尝试过于频繁，请稍后再试。")
	}
	s.loginLimit.attempts++
	s.loginLimit.mu.Unlock()
	// A trusted setup credential remains usable in the same password-shaped UI.
	// Failed database lookups are not converted into a password mismatch.
	principal, e := s.Authenticate(ctx, value)
	if e == nil {
		if !principal.Has("admin") {
			return model.Principal{}, Fail(403, "scope_denied", "此凭据不能创建管理员设备会话。")
		}
		return principal, nil
	}
	var problem *Error
	if !errors.As(e, &problem) || problem.Code != "unauthorized" {
		return model.Principal{}, e
	}
	if in.Password == "" || s.password == nil {
		return model.Principal{}, Fail(401, "unauthorized", "密码或凭据无效。")
	}
	select {
	case s.loginLimit.slots <- struct{}{}:
		defer func() { <-s.loginLimit.slots }()
	default:
		return model.Principal{}, Fail(429, "login_busy", "登录验证繁忙，请稍后再试。")
	}
	if e := ctx.Err(); e != nil {
		return model.Principal{}, e
	}
	key, e := pbkdf2.Key(sha256.New, in.Password, s.password.salt, passwordIterations, 32)
	if e != nil {
		return model.Principal{}, e
	}
	if e = ctx.Err(); e != nil {
		return model.Principal{}, e
	}
	if subtle.ConstantTimeCompare(key, s.password.key) != 1 {
		return model.Principal{}, Fail(401, "unauthorized", "密码或凭据无效。")
	}
	return model.Principal{ID: "owner", Label: "Owner", Scopes: []string{"admin"}, ExpiresAt: time.Now().UTC().Add(24 * time.Hour)}, nil
}
