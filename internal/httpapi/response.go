package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5/middleware"

	"usahainaja/backend/internal/app"
)

type dataEnvelope struct {
	Success bool `json:"success"`
	Data    any  `json:"data"`
}

type errorBody struct {
	Code      string            `json:"code"`
	Message   string            `json:"message"`
	Fields    map[string]string `json:"fields,omitempty"`
	RequestID string            `json:"request_id,omitempty"`
}

type errorEnvelope struct {
	Success bool      `json:"success"`
	Error   errorBody `json:"error"`
}

func writeData(w http.ResponseWriter, status int, value any) {
	writeJSON(w, status, dataEnvelope{Success: true, Data: value})
}

func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string, fields map[string]string) {
	writeJSON(w, status, errorEnvelope{Success: false, Error: errorBody{
		Code: code, Message: message, Fields: fields, RequestID: middleware.GetReqID(r.Context()),
	}})
}

func writeAppError(w http.ResponseWriter, r *http.Request, err error) {
	var appErr *app.Error
	if !errors.As(err, &appErr) {
		slog.Error("unhandled request error", "request_id", middleware.GetReqID(r.Context()), "error", err)
		writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Terjadi kesalahan pada server.", nil)
		return
	}
	status := statusForCode(appErr.Code)
	if status >= 500 {
		slog.Error("request failed", "request_id", middleware.GetReqID(r.Context()), "code", appErr.Code, "error", appErr)
	}
	writeError(w, r, status, appErr.Code, appErr.Message, appErr.Fields)
}

func statusForCode(code string) int {
	switch code {
	case "VALIDATION_ERROR":
		return http.StatusUnprocessableEntity
	case "UNAUTHENTICATED", "UNAUTHORIZED":
		return http.StatusUnauthorized
	case "BUSINESS_ACCESS_DENIED", "STOCK_TRACKING_DISABLED", "PERMISSION_DENIED":
		return http.StatusForbidden
	case "PRODUCT_OR_LOCATION_NOT_FOUND":
		return http.StatusNotFound
	case "ACTIVE_BUSINESS_REQUIRED", "EMAIL_ALREADY_EXISTS", "PRODUCT_CONFLICT", "OPENING_STOCK_ALREADY_RECORDED":
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Error("encode JSON response", "error", err)
	}
}
