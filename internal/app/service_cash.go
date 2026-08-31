package app

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"
)

func (s *Service) ListCashAccounts(ctx context.Context, businessID string) ([]CashAccount, error) {
	items, err := s.repo.ListCashAccounts(ctx, businessID)
	if err != nil {
		return nil, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat memuat akun kas.", Cause: err}
	}
	return items, nil
}

type CreateCashAccountInput struct {
	Name        string `json:"name"`
	AccountType string `json:"account_type"`
	Balance     string `json:"balance"`
	IsDefault   *bool  `json:"is_default"`
}

func (s *Service) CreateCashAccount(ctx context.Context, businessID string, in CreateCashAccountInput) (CashAccount, error) {
	name := strings.TrimSpace(in.Name)
	fields := map[string]string{}
	if name == "" || utf8.RuneCountInString(name) > 100 {
		fields["name"] = "Nama akun wajib diisi dan maksimal 100 karakter."
	}
	accountType := strings.ToUpper(strings.TrimSpace(in.AccountType))
	if accountType == "" {
		accountType = "CASH"
	}
	if !oneOf(accountType, "CASH", "BANK", "EWALLET") {
		fields["account_type"] = "Tipe akun tidak didukung."
	}
	balance, err := normalizeDecimal(in.Balance, 2, 16, true)
	if err != nil {
		fields["balance"] = err.Error()
	}
	if len(fields) != 0 {
		return CashAccount{}, validationError(fields)
	}

	isDefault := false
	if in.IsDefault != nil {
		isDefault = *in.IsDefault
	}

	account, err := s.repo.CreateCashAccount(ctx, businessID, NewCashAccount{
		Name: name, AccountType: accountType, Balance: balance, IsDefault: isDefault,
	})
	if errors.Is(err, ErrConflict) {
		return CashAccount{}, &Error{Code: "CONFLICT", Message: "Akun kas dengan kode tersebut sudah ada."}
	}
	if err != nil {
		return CashAccount{}, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat membuat akun kas.", Cause: err}
	}
	return account, nil
}
