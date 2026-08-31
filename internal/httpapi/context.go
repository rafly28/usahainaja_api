package httpapi

import (
	"context"
	"log/slog"
	"net/http"

	"usahainaja/backend/internal/app"
)

type contextKey int

const (
	sessionContextKey contextKey = iota
	businessContextKey
)

func (a *API) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := a.service.Authenticate(r.Context(), a.rawCookie(r))
		if err != nil {
			a.clearSessionCookie(w)
			writeAppError(w, r, err)
			return
		}
		ctx := context.WithValue(r.Context(), sessionContextKey, session)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (a *API) requireCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provided := normalizedHeader(r.Header.Get("X-CSRF-Token"))
		session := sessionFrom(r.Context())
		if !a.service.ValidateCSRF(session, provided) {
			slog.Error("CSRF validation failed", "provided", provided, "expected", session.CSRFToken)
			writeError(w, r, http.StatusForbidden, "CSRF_TOKEN_INVALID", "Token CSRF tidak valid.", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *API) requireBusiness(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		business, err := a.service.BusinessContext(r.Context(), sessionFrom(r.Context()))
		if err != nil {
			writeAppError(w, r, err)
			return
		}
		ctx := context.WithValue(r.Context(), businessContextKey, business)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (a *API) requireRole(allowed ...string) func(http.Handler) http.Handler {
	roles := make(map[string]struct{}, len(allowed))
	for _, role := range allowed {
		roles[role] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := roles[businessFrom(r.Context()).Role]; !ok {
				writeError(w, r, http.StatusForbidden, "PERMISSION_DENIED", "Anda tidak memiliki izin untuk tindakan ini.", nil)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (a *API) requireModule(module string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, enabled := range businessFrom(r.Context()).EnabledModules {
				if enabled == module {
					next.ServeHTTP(w, r)
					return
				}
			}
			writeError(w, r, http.StatusForbidden, "MODULE_DISABLED", "Modul bisnis ini tidak aktif.", map[string]string{"module": module})
		})
	}
}

func sessionFrom(ctx context.Context) app.Session {
	session, _ := ctx.Value(sessionContextKey).(app.Session)
	return session
}

func businessFrom(ctx context.Context) app.BusinessContext {
	business, _ := ctx.Value(businessContextKey).(app.BusinessContext)
	return business
}
