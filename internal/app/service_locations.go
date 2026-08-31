package app

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"
)

func (s *Service) ListLocations(ctx context.Context, businessID string) ([]Location, error) {
	items, err := s.repo.ListLocations(ctx, businessID)
	if err != nil {
		return nil, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat memuat lokasi.", Cause: err}
	}
	return items, nil
}

type CreateLocationInput struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Address   string `json:"address"`
	IsDefault bool   `json:"is_default"`
}
type UpdateLocationInput struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Address   string `json:"address"`
	Status    string `json:"status"`
	IsDefault bool   `json:"is_default"`
}

func (s *Service) CreateLocation(ctx context.Context, session Session, business BusinessContext, in CreateLocationInput) (Location, error) {
	if !oneOf(business.Role, "OWNER", "ADMIN") {
		return Location{}, &Error{Code: "PERMISSION_DENIED", Message: "Anda tidak memiliki izin untuk mengelola lokasi."}
	}
	input, err := normalizeLocationInput(in.Name, in.Type, in.Address, in.IsDefault)
	if err != nil {
		return Location{}, err
	}
	item, err := s.repo.CreateLocation(ctx, business.ID, session.UserID, input)
	if errors.Is(err, ErrConflict) {
		return Location{}, &Error{Code: "LOCATION_CONFLICT", Message: "Lokasi tidak dapat dijadikan default."}
	}
	if err != nil {
		return Location{}, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat membuat lokasi.", Cause: err}
	}
	return item, nil
}
func (s *Service) UpdateLocation(ctx context.Context, session Session, business BusinessContext, code string, in UpdateLocationInput) (Location, error) {
	if !oneOf(business.Role, "OWNER", "ADMIN") {
		return Location{}, &Error{Code: "PERMISSION_DENIED", Message: "Anda tidak memiliki izin untuk mengelola lokasi."}
	}
	code = strings.ToUpper(strings.TrimSpace(code))
	if !codePattern.MatchString(code) {
		return Location{}, validationError(map[string]string{"code": "Kode lokasi tidak valid."})
	}
	input, err := normalizeLocationInput(in.Name, in.Type, in.Address, in.IsDefault)
	if err != nil {
		return Location{}, err
	}
	status := strings.ToUpper(strings.TrimSpace(in.Status))
	if status == "" {
		status = "ACTIVE"
	}
	if !oneOf(status, "ACTIVE", "INACTIVE") {
		return Location{}, validationError(map[string]string{"status": "Status lokasi tidak valid."})
	}
	if status == "INACTIVE" && input.IsDefault {
		return Location{}, validationError(map[string]string{"status": "Lokasi default harus tetap aktif."})
	}
	item, err := s.repo.UpdateLocation(ctx, business.ID, session.UserID, code, status, input)
	if errors.Is(err, ErrNotFound) {
		return Location{}, &Error{Code: "LOCATION_NOT_FOUND", Message: "Lokasi tidak ditemukan atau default aktif tidak tersedia."}
	}
	if errors.Is(err, ErrConflict) {
		return Location{}, &Error{Code: "LOCATION_CONFLICT", Message: "Lokasi tidak dapat dijadikan default."}
	}
	if err != nil {
		return Location{}, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat mengubah lokasi.", Cause: err}
	}
	return item, nil
}
func normalizeLocationInput(rawName, rawType, rawAddress string, isDefault bool) (NewLocation, error) {
	name := strings.TrimSpace(rawName)
	kind := strings.ToUpper(strings.TrimSpace(rawType))
	address := strings.TrimSpace(rawAddress)
	fields := map[string]string{}
	if name == "" || utf8.RuneCountInString(name) > 150 {
		fields["name"] = "Nama lokasi wajib diisi dan maksimal 150 karakter."
	}
	if kind == "" {
		kind = "STORE"
	}
	if !oneOf(kind, "STORE", "WAREHOUSE", "BOOTH", "EVENT_VENUE", "OTHER") {
		fields["type"] = "Tipe lokasi tidak didukung."
	}
	if len(fields) != 0 {
		return NewLocation{}, validationError(fields)
	}
	return NewLocation{Name: name, Type: kind, Address: address, IsDefault: isDefault}, nil
}
