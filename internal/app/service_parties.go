package app

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"
)

func (s *Service) ListParties(ctx context.Context, businessID string) ([]Party, error) {
	items, err := s.repo.ListParties(ctx, businessID)
	if err != nil {
		return nil, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat memuat party.", Cause: err}
	}
	return items, nil
}

type CreatePartyInput struct {
	PartyType     string         `json:"party_type"`
	DisplayName   string         `json:"display_name"`
	LegalName     string         `json:"legal_name,omitempty"`
	Notes         string         `json:"notes,omitempty"`
	Relationships []string       `json:"relationships"`
	Contacts      []PartyContact `json:"contacts"`
	Addresses     []PartyAddress `json:"addresses"`
}

func (s *Service) CreateParty(ctx context.Context, session Session, business BusinessContext, in CreatePartyInput) (Party, error) {
	if !oneOf(business.Role, "OWNER", "ADMIN") {
		return Party{}, &Error{Code: "PERMISSION_DENIED", Message: "Anda tidak memiliki izin untuk mengelola party."}
	}
	input, err := normalizeParty(in)
	if err != nil {
		return Party{}, err
	}
	item, err := s.repo.CreateParty(ctx, business.ID, session.UserID, input)
	if errors.Is(err, ErrConflict) {
		return Party{}, &Error{Code: "PARTY_CONFLICT", Message: "Party tidak dapat disimpan."}
	}
	if err != nil {
		return Party{}, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat membuat party.", Cause: err}
	}
	return item, nil
}

type UpdatePartyInput struct {
	PartyType     string         `json:"party_type"`
	DisplayName   string         `json:"display_name"`
	LegalName     string         `json:"legal_name,omitempty"`
	Notes         string         `json:"notes,omitempty"`
	Status        string         `json:"status"`
	Relationships []string       `json:"relationships"`
	Contacts      []PartyContact `json:"contacts"`
	Addresses     []PartyAddress `json:"addresses"`
}

func (s *Service) UpdateParty(ctx context.Context, session Session, business BusinessContext, code string, in UpdatePartyInput) (Party, error) {
	if !oneOf(business.Role, "OWNER", "ADMIN") {
		return Party{}, &Error{Code: "PERMISSION_DENIED", Message: "Anda tidak memiliki izin untuk mengelola party."}
	}
	code = strings.ToUpper(strings.TrimSpace(code))
	if !codePattern.MatchString(code) {
		return Party{}, validationError(map[string]string{"code": "Kode party tidak valid."})
	}
	input, err := normalizeParty(CreatePartyInput{
		PartyType: in.PartyType, DisplayName: in.DisplayName, LegalName: in.LegalName, Notes: in.Notes,
		Relationships: in.Relationships, Contacts: in.Contacts, Addresses: in.Addresses,
	})
	if err != nil {
		return Party{}, err
	}
	status := strings.ToUpper(strings.TrimSpace(in.Status))
	if status == "" {
		status = "ACTIVE"
	}
	if !oneOf(status, "ACTIVE", "INACTIVE") {
		return Party{}, validationError(map[string]string{"status": "Status party tidak valid."})
	}
	item, err := s.repo.UpdateParty(ctx, business.ID, session.UserID, code, status, input)
	if errors.Is(err, ErrNotFound) {
		return Party{}, &Error{Code: "PARTY_NOT_FOUND", Message: "Party tidak ditemukan."}
	}
	if err != nil {
		return Party{}, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat mengubah party.", Cause: err}
	}
	return item, nil
}
func normalizeParty(in CreatePartyInput) (NewParty, error) {
	p := NewParty{PartyType: strings.ToUpper(strings.TrimSpace(in.PartyType)), DisplayName: strings.TrimSpace(in.DisplayName), LegalName: strings.TrimSpace(in.LegalName), Notes: strings.TrimSpace(in.Notes), Contacts: in.Contacts, Addresses: in.Addresses}
	fields := map[string]string{}
	if !oneOf(p.PartyType, "PERSON", "ORGANIZATION") {
		fields["party_type"] = "Tipe party tidak didukung."
	}
	if p.DisplayName == "" || utf8.RuneCountInString(p.DisplayName) > 150 {
		fields["display_name"] = "Nama party wajib diisi dan maksimal 150 karakter."
	}
	seen := map[string]struct{}{}
	for _, r := range in.Relationships {
		r = strings.ToUpper(strings.TrimSpace(r))
		if !oneOf(r, "CUSTOMER", "SUPPLIER", "PARTNER", "CLIENT", "TALENT", "EMPLOYEE", "OTHER") {
			fields["relationships"] = "Relationship tidak didukung."
		}
		if r != "" {
			seen[r] = struct{}{}
		}
	}
	for r := range seen {
		p.Relationships = append(p.Relationships, r)
	}
	primary := 0
	for i := range p.Contacts {
		p.Contacts[i].Type = strings.ToUpper(strings.TrimSpace(p.Contacts[i].Type))
		p.Contacts[i].Value = strings.TrimSpace(p.Contacts[i].Value)
		if !oneOf(p.Contacts[i].Type, "PHONE", "WHATSAPP", "EMAIL", "OTHER") || p.Contacts[i].Value == "" {
			fields["contacts"] = "Channel kontak tidak valid."
		}
		if p.Contacts[i].IsPrimary {
			primary++
		}
	}
	if primary > 1 {
		fields["contacts"] = "Hanya satu kontak utama."
	}
	if len(fields) != 0 {
		return NewParty{}, validationError(fields)
	}
	return p, nil
}
