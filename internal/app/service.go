package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

var (
	codePattern     = regexp.MustCompile(`^[A-Z0-9][A-Z0-9_-]{1,31}$`)
	currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)
)

type Service struct {
	repo       Repository
	sessionTTL time.Duration
	bcryptCost int
	now        func() time.Time
	random     func(int) ([]byte, error)
}

func NewService(repo Repository, sessionTTL time.Duration, bcryptCost int) *Service {
	return &Service{
		repo:       repo,
		sessionTTL: sessionTTL,
		bcryptCost: bcryptCost,
		now:        time.Now,
		random:     randomBytes,
	}
}

type RegisterInput struct{ Name, Email, Password string }
type LoginInput struct{ Email, Password string }

type AuthResult struct {
	User           User      `json:"user"`
	ActiveBusiness *Business `json:"active_business"`
	CSRFToken      string    `json:"csrf_token"`
}

func (s *Service) Ping(ctx context.Context) error { return s.repo.Ping(ctx) }

func (s *Service) Register(ctx context.Context, in RegisterInput, meta SessionMeta, previousToken string) (AuthResult, SessionCredentials, error) {
	name, email, fields := validateIdentity(in.Name, in.Email, in.Password)
	if len(fields) != 0 {
		return AuthResult{}, SessionCredentials{}, validationError(fields)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), s.bcryptCost)
	if err != nil {
		return AuthResult{}, SessionCredentials{}, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat membuat akun.", Cause: err}
	}
	userCode, err := s.newCode("USR")
	if err != nil {
		return AuthResult{}, SessionCredentials{}, internalRandomError(err)
	}
	newSession, credentials, err := s.newSession(meta)
	if err != nil {
		return AuthResult{}, SessionCredentials{}, internalRandomError(err)
	}
	previousHash := tokenHashOrNil(previousToken)
	record, session, err := s.repo.CreateUserAndSession(ctx, NewUser{
		Code: userCode, Name: name, Email: email, PasswordHash: string(hash),
	}, newSession, previousHash)
	if errors.Is(err, ErrConflict) {
		return AuthResult{}, SessionCredentials{}, &Error{Code: "EMAIL_ALREADY_EXISTS", Message: "Email sudah terdaftar.", Fields: map[string]string{"email": "Email sudah terdaftar."}}
	}
	if err != nil {
		return AuthResult{}, SessionCredentials{}, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat membuat akun.", Cause: err}
	}
	return AuthResult{User: record.User, ActiveBusiness: nil, CSRFToken: credentials.CSRFToken}, withExpiry(credentials, session.ExpiresAt), nil
}

func (s *Service) Login(ctx context.Context, in LoginInput, meta SessionMeta, previousToken string) (AuthResult, SessionCredentials, error) {
	email := strings.ToLower(strings.TrimSpace(in.Email))
	if email == "" || in.Password == "" {
		return AuthResult{}, SessionCredentials{}, &Error{Code: "INVALID_CREDENTIALS", Message: "Email atau password salah."}
	}
	record, err := s.repo.FindUserByEmail(ctx, email)
	if errors.Is(err, ErrNotFound) {
		return AuthResult{}, SessionCredentials{}, &Error{Code: "INVALID_CREDENTIALS", Message: "Email atau password salah."}
	}
	if err != nil {
		return AuthResult{}, SessionCredentials{}, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat masuk.", Cause: err}
	}
	if bcrypt.CompareHashAndPassword([]byte(record.PasswordHash), []byte(in.Password)) != nil {
		return AuthResult{}, SessionCredentials{}, &Error{Code: "INVALID_CREDENTIALS", Message: "Email atau password salah."}
	}
	newSession, credentials, err := s.newSession(meta)
	if err != nil {
		return AuthResult{}, SessionCredentials{}, internalRandomError(err)
	}
	session, err := s.repo.ReplaceSession(ctx, record.ID, newSession, tokenHashOrNil(previousToken))
	if err != nil {
		return AuthResult{}, SessionCredentials{}, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat masuk.", Cause: err}
	}

	var active *Business
	if session.ActiveBusinessID != nil {
		business, businessErr := s.repo.GetBusinessContext(ctx, session.UserID, *session.ActiveBusinessID)
		if businessErr == nil {
			value := business.Business
			active = &value
		}
	}
	return AuthResult{User: record.User, ActiveBusiness: active, CSRFToken: credentials.CSRFToken}, withExpiry(credentials, session.ExpiresAt), nil
}

func (s *Service) Authenticate(ctx context.Context, rawToken string) (Session, error) {
	if rawToken == "" {
		return Session{}, &Error{Code: "UNAUTHENTICATED", Message: "Silakan masuk terlebih dahulu."}
	}
	session, err := s.repo.LoadSession(ctx, hashToken(rawToken))
	if errors.Is(err, ErrNotFound) {
		return Session{}, &Error{Code: "UNAUTHENTICATED", Message: "Sesi tidak valid atau sudah berakhir."}
	}
	if err != nil {
		return Session{}, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat memvalidasi sesi.", Cause: err}
	}
	return session, nil
}

func (s *Service) Logout(ctx context.Context, sessionID string) error {
	if err := s.repo.DeleteSession(ctx, sessionID); err != nil {
		return &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat keluar.", Cause: err}
	}
	return nil
}

func (s *Service) ValidateCSRF(session Session, provided string) bool {
	if provided == "" || len(provided) != len(session.CSRFToken) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(session.CSRFToken)) == 1
}

func validateIdentity(rawName, rawEmail, password string) (string, string, map[string]string) {
	name := strings.TrimSpace(rawName)
	email := strings.ToLower(strings.TrimSpace(rawEmail))
	fields := map[string]string{}
	if name == "" || utf8.RuneCountInString(name) > 150 {
		fields["name"] = "Nama wajib diisi dan maksimal 150 karakter."
	}
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email || utf8.RuneCountInString(email) > 254 {
		fields["email"] = "Format email tidak valid."
	}
	if len(password) < 8 {
		fields["password"] = "Password minimal 8 karakter."
	} else if len(password) > 72 {
		fields["password"] = "Password maksimal 72 byte."
	}
	return name, email, fields
}

func (s *Service) newSession(meta SessionMeta) (NewSession, SessionCredentials, error) {
	tokenBytes, err := s.random(32)
	if err != nil {
		return NewSession{}, SessionCredentials{}, err
	}
	csrfBytes, err := s.random(32)
	if err != nil {
		return NewSession{}, SessionCredentials{}, err
	}
	rawToken := base64.RawURLEncoding.EncodeToString(tokenBytes)
	csrfToken := base64.RawURLEncoding.EncodeToString(csrfBytes)
	expires := s.now().UTC().Add(s.sessionTTL)
	return NewSession{
		TokenHash: hashToken(rawToken), CSRFToken: csrfToken, ExpiresAt: expires, Meta: meta,
	}, SessionCredentials{Token: rawToken, CSRFToken: csrfToken, ExpiresAt: expires}, nil
}

func (s *Service) newCode(prefix string) (string, error) {
	random, err := s.random(8)
	if err != nil {
		return "", err
	}
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(random)
	return fmt.Sprintf("%s-%s", prefix, encoded), nil
}

func hashToken(raw string) []byte {
	sum := sha256.Sum256([]byte(raw))
	return sum[:]
}

func tokenHashOrNil(raw string) []byte {
	if raw == "" {
		return nil
	}
	return hashToken(raw)
}

func randomBytes(size int) ([]byte, error) {
	value := make([]byte, size)
	_, err := rand.Read(value)
	return value, err
}

func withExpiry(credentials SessionCredentials, expiresAt time.Time) SessionCredentials {
	credentials.ExpiresAt = expiresAt
	return credentials
}

func internalRandomError(err error) *Error {
	return &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat membuat kredensial aman.", Cause: err}
}
