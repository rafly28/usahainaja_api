package app

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"
)

func (s *Service) ListUnits(ctx context.Context, businessID string) ([]Unit, error) {
	items, err := s.repo.ListUnits(ctx, businessID)
	if err != nil {
		return nil, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat memuat satuan.", Cause: err}
	}
	return items, nil
}

type CreateUnitInput struct {
	Name     string `json:"name"`
	Symbol   string `json:"symbol"`
	UnitType string `json:"unit_type"`
}
type UpdateUnitInput struct {
	Name     string `json:"name"`
	Symbol   string `json:"symbol"`
	UnitType string `json:"unit_type"`
	Status   string `json:"status"`
}

func (s *Service) CreateUnit(ctx context.Context, session Session, business BusinessContext, in CreateUnitInput) (Unit, error) {
	if !oneOf(business.Role, "OWNER", "ADMIN") {
		return Unit{}, &Error{Code: "PERMISSION_DENIED", Message: "Anda tidak memiliki izin untuk mengelola satuan."}
	}
	input, err := normalizeUnitInput(in.Name, in.Symbol, in.UnitType)
	if err != nil {
		return Unit{}, err
	}
	item, err := s.repo.CreateUnit(ctx, business.ID, session.UserID, input)
	if errors.Is(err, ErrConflict) {
		return Unit{}, &Error{Code: "UNIT_CONFLICT", Message: "Simbol satuan sudah digunakan."}
	}
	if err != nil {
		return Unit{}, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat membuat satuan.", Cause: err}
	}
	return item, nil
}

func (s *Service) UpdateUnit(ctx context.Context, session Session, business BusinessContext, code string, in UpdateUnitInput) (Unit, error) {
	if !oneOf(business.Role, "OWNER", "ADMIN") {
		return Unit{}, &Error{Code: "PERMISSION_DENIED", Message: "Anda tidak memiliki izin untuk mengelola satuan."}
	}
	code = strings.ToUpper(strings.TrimSpace(code))
	if !codePattern.MatchString(code) {
		return Unit{}, validationError(map[string]string{"code": "Kode satuan tidak valid."})
	}
	input, err := normalizeUnitInput(in.Name, in.Symbol, in.UnitType)
	if err != nil {
		return Unit{}, err
	}
	status := strings.ToUpper(strings.TrimSpace(in.Status))
	if status == "" {
		status = "ACTIVE"
	}
	if !oneOf(status, "ACTIVE", "INACTIVE") {
		return Unit{}, validationError(map[string]string{"status": "Status satuan tidak valid."})
	}
	item, err := s.repo.UpdateUnit(ctx, business.ID, session.UserID, code, status, input)
	if errors.Is(err, ErrNotFound) {
		return Unit{}, &Error{Code: "UNIT_NOT_FOUND", Message: "Satuan tidak ditemukan."}
	}
	if errors.Is(err, ErrConflict) {
		return Unit{}, &Error{Code: "UNIT_CONFLICT", Message: "Simbol satuan sudah digunakan."}
	}
	if err != nil {
		return Unit{}, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat mengubah satuan.", Cause: err}
	}
	return item, nil
}

func normalizeUnitInput(rawName, rawSymbol, rawType string) (NewUnit, error) {
	name := strings.TrimSpace(rawName)
	symbol := strings.ToUpper(strings.TrimSpace(rawSymbol))
	unitType := strings.ToUpper(strings.TrimSpace(rawType))
	fields := map[string]string{}
	if name == "" || utf8.RuneCountInString(name) > 100 {
		fields["name"] = "Nama satuan wajib diisi dan maksimal 100 karakter."
	}
	if symbol == "" || utf8.RuneCountInString(symbol) > 30 {
		fields["symbol"] = "Simbol satuan wajib diisi dan maksimal 30 karakter."
	}
	if !oneOf(unitType, "COUNT", "WEIGHT", "VOLUME", "TIME", "OTHER") {
		fields["unit_type"] = "Tipe satuan tidak didukung."
	}
	if len(fields) != 0 {
		return NewUnit{}, validationError(fields)
	}
	return NewUnit{Name: name, Symbol: symbol, UnitType: unitType}, nil
}
