package app

import (
	"context"
	"errors"
	"strings"
)

type CreateUnitConversionInput struct{ ProductCode, FromUnitCode, ToUnitCode, Multiplier string }

func (s *Service) ListUnitConversions(ctx context.Context, businessID string) ([]UnitConversion, error) {
	items, err := s.repo.ListUnitConversions(ctx, businessID)
	if err != nil {
		return nil, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat memuat konversi satuan.", Cause: err}
	}
	return items, nil
}

func (s *Service) CreateUnitConversion(ctx context.Context, session Session, business BusinessContext, in CreateUnitConversionInput) (UnitConversion, error) {
	if !oneOf(business.Role, "OWNER", "ADMIN") {
		return UnitConversion{}, &Error{Code: "PERMISSION_DENIED", Message: "Anda tidak memiliki izin untuk mengelola konversi satuan."}
	}
	input := NewUnitConversion{ProductCode: strings.ToUpper(strings.TrimSpace(in.ProductCode)), FromUnitCode: strings.ToUpper(strings.TrimSpace(in.FromUnitCode)), ToUnitCode: strings.ToUpper(strings.TrimSpace(in.ToUnitCode)), Multiplier: strings.TrimSpace(in.Multiplier)}
	fields := map[string]string{}
	if input.ProductCode != "" && !codePattern.MatchString(input.ProductCode) {
		fields["product_code"] = "Kode produk tidak valid."
	}
	if !codePattern.MatchString(input.FromUnitCode) || !codePattern.MatchString(input.ToUnitCode) {
		fields["unit_code"] = "Kode satuan tidak valid."
	}
	if input.FromUnitCode == input.ToUnitCode {
		fields["to_unit_code"] = "Satuan asal dan tujuan harus berbeda."
	}
	if _, err := normalizeDecimal(input.Multiplier, 6, 12, true); err != nil {
		fields["multiplier"] = err.Error()
	}
	if len(fields) != 0 {
		return UnitConversion{}, validationError(fields)
	}
	item, err := s.repo.CreateUnitConversion(ctx, business.ID, session.UserID, input)
	if errors.Is(err, ErrNotFound) {
		return UnitConversion{}, validationError(map[string]string{"unit_code": "Produk atau satuan aktif tidak ditemukan."})
	}
	if errors.Is(err, ErrConflict) {
		return UnitConversion{}, &Error{Code: "UNIT_CONVERSION_CONFLICT", Message: "Konversi satuan tersebut sudah ada."}
	}
	if err != nil {
		return UnitConversion{}, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat menyimpan konversi satuan.", Cause: err}
	}
	return item, nil
}
