package app

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"
)

func (s *Service) ListContacts(ctx context.Context, businessID string) ([]Contact, error) {
	items, err := s.repo.ListContacts(ctx, businessID)
	if err != nil {
		return nil, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat memuat kontak.", Cause: err}
	}
	return items, nil
}

type CreateContactInput struct {
	ContactType, Name, Email, Phone, Address string
}

func (s *Service) CreateContact(ctx context.Context, businessID string, in CreateContactInput) (Contact, error) {
	name := strings.TrimSpace(in.Name)
	fields := map[string]string{}
	if name == "" || utf8.RuneCountInString(name) > 150 {
		fields["name"] = "Nama kontak wajib diisi dan maksimal 150 karakter."
	}
	contactType := strings.ToUpper(strings.TrimSpace(in.ContactType))
	if contactType == "" {
		contactType = "CUSTOMER"
	}
	if !oneOf(contactType, "CUSTOMER", "SUPPLIER", "BOTH") {
		fields["contact_type"] = "Tipe kontak tidak didukung."
	}
	email := strings.ToLower(strings.TrimSpace(in.Email))
	if email != "" && utf8.RuneCountInString(email) > 254 {
		fields["email"] = "Format email tidak valid."
	}
	phone := strings.TrimSpace(in.Phone)
	if utf8.RuneCountInString(phone) > 50 {
		fields["phone"] = "Nomor telepon maksimal 50 karakter."
	}
	address := strings.TrimSpace(in.Address)
	if len(fields) != 0 {
		return Contact{}, validationError(fields)
	}

	contact, err := s.repo.CreateContact(ctx, businessID, NewContact{
		ContactType: contactType, Name: name, Email: email, Phone: phone, Address: address,
	})
	if errors.Is(err, ErrConflict) {
		return Contact{}, &Error{Code: "CONFLICT", Message: "Kontak dengan kode tersebut sudah ada."}
	}
	if err != nil {
		return Contact{}, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat membuat kontak.", Cause: err}
	}
	return contact, nil
}
