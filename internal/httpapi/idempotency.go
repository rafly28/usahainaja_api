package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

func paginationParams(r *http.Request) (page, limit int) {
	page = 1
	limit = 25
	if raw := strings.TrimSpace(r.URL.Query().Get("page")); raw != "" {
		if value, err := strconv.Atoi(raw); err == nil && value >= 1 {
			page = value
		}
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if value, err := strconv.Atoi(raw); err == nil && value >= 1 {
			limit = value
			if limit > 100 {
				limit = 100
			}
		}
	}
	return page, limit
}

type captureWriter struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (c *captureWriter) WriteHeader(status int) {
	c.status = status
	c.ResponseWriter.WriteHeader(status)
}

func (c *captureWriter) Write(data []byte) (int, error) {
	c.body.Write(data)
	return c.ResponseWriter.Write(data)
}

// requireIdempotency enforces the Transaction Contract idempotency rule on
// money/stock mutations: same key + same payload replays the stored
// response, same key + different payload is a 409 conflict.
func (a *API) requireIdempotency(operation string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rawKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
			key, err := uuid.Parse(rawKey)
			if rawKey == "" || err != nil {
				writeError(w, r, http.StatusBadRequest, "IDEMPOTENCY_KEY_REQUIRED", "Header Idempotency-Key UUID wajib dikirim.", nil)
				return
			}

			var body []byte
			if r.Body != nil {
				body, _ = io.ReadAll(r.Body)
				r.Body = io.NopCloser(bytes.NewReader(body))
			}
			sum := sha256.Sum256(body)
			hash := hex.EncodeToString(sum[:])

			business := businessFrom(r.Context())
			storedStatus, storedBody, storedHash, found, err := a.service.FindIdempotency(r.Context(), business.ID, key.String())
			if err != nil {
				writeAppError(w, r, err)
				return
			}
			if found {
				if storedHash != hash {
					writeError(w, r, http.StatusConflict, "IDEMPOTENCY_CONFLICT", "Idempotency-Key sudah dipakai dengan payload berbeda.", nil)
					return
				}
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(storedStatus)
				_, _ = w.Write(storedBody)
				return
			}

			capture := &captureWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(capture, r)

			if capture.status < 500 {
				_ = a.service.StoreIdempotency(r.Context(), business.ID, sessionFrom(r.Context()).UserID,
					operation, r.URL.Path, key.String(), hash, capture.status, capture.body.Bytes())
			}
		})
	}
}
