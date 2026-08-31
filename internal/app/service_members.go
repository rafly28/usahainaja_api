package app

import (
	"context"
	"errors"
	"net/mail"
	"strings"
)

var memberRoles = []string{"OWNER", "ADMIN", "CASHIER", "STAFF", "VIEWER"}

type InviteBusinessMemberInput struct{ Email, Role string }
type UpdateBusinessMemberInput struct{ Role, Status string }

func (s *Service) ListBusinessMembers(ctx context.Context, business BusinessContext) ([]BusinessMember, error) {
	if !oneOf(business.Role, "OWNER", "ADMIN") {
		return nil, &Error{Code: "PERMISSION_DENIED", Message: "Anda tidak memiliki izin untuk melihat anggota usaha."}
	}
	items, err := s.repo.ListBusinessMembers(ctx, business.ID)
	if err != nil {
		return nil, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat memuat anggota usaha.", Cause: err}
	}
	return items, nil
}

func (s *Service) InviteBusinessMember(ctx context.Context, session Session, business BusinessContext, in InviteBusinessMemberInput) (BusinessMember, error) {
	if !oneOf(business.Role, "OWNER", "ADMIN") {
		return BusinessMember{}, &Error{Code: "PERMISSION_DENIED", Message: "Anda tidak memiliki izin untuk mengundang anggota."}
	}
	email := strings.ToLower(strings.TrimSpace(in.Email))
	role := strings.ToUpper(strings.TrimSpace(in.Role))
	fields := map[string]string{}
	if email == "" {
		fields["email"] = "Email pengguna terdaftar wajib diisi."
	} else if parsed, err := mail.ParseAddress(email); err != nil || !strings.EqualFold(parsed.Address, email) {
		fields["email"] = "Email pengguna terdaftar wajib diisi."
	}
	if !oneOf(role, memberRoles...) {
		fields["role"] = "Role anggota tidak didukung."
	}
	if role == "OWNER" && business.Role != "OWNER" {
		fields["role"] = "Hanya Owner yang dapat menetapkan role Owner."
	}
	if len(fields) != 0 {
		return BusinessMember{}, validationError(fields)
	}
	item, err := s.repo.InviteBusinessMember(ctx, business.ID, session.UserID, email, role)
	if errors.Is(err, ErrNotFound) {
		return BusinessMember{}, validationError(map[string]string{"email": "Pengguna dengan email tersebut belum terdaftar."})
	}
	if errors.Is(err, ErrConflict) {
		return BusinessMember{}, &Error{Code: "MEMBER_CONFLICT", Message: "Pengguna sudah menjadi anggota usaha ini."}
	}
	if err != nil {
		return BusinessMember{}, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat mengundang anggota.", Cause: err}
	}
	return item, nil
}

func (s *Service) UpdateBusinessMember(ctx context.Context, session Session, business BusinessContext, userCode string, in UpdateBusinessMemberInput) (BusinessMember, error) {
	if !oneOf(business.Role, "OWNER", "ADMIN") {
		return BusinessMember{}, &Error{Code: "PERMISSION_DENIED", Message: "Anda tidak memiliki izin untuk mengubah anggota."}
	}
	userCode = strings.ToUpper(strings.TrimSpace(userCode))
	role := strings.ToUpper(strings.TrimSpace(in.Role))
	status := strings.ToUpper(strings.TrimSpace(in.Status))
	fields := map[string]string{}
	if !codePattern.MatchString(userCode) {
		fields["user_code"] = "Kode pengguna tidak valid."
	}
	if !oneOf(role, memberRoles...) {
		fields["role"] = "Role anggota tidak didukung."
	}
	if !oneOf(status, "INVITED", "ACTIVE", "INACTIVE") {
		fields["status"] = "Status anggota tidak didukung."
	}
	if business.Role != "OWNER" && role == "OWNER" {
		fields["role"] = "Hanya Owner yang dapat menetapkan role Owner."
	}
	if len(fields) != 0 {
		return BusinessMember{}, validationError(fields)
	}
	item, err := s.repo.UpdateBusinessMember(ctx, business.ID, session.UserID, userCode, role, status)
	if errors.Is(err, ErrNotFound) {
		return BusinessMember{}, &Error{Code: "MEMBER_NOT_FOUND", Message: "Anggota tidak ditemukan."}
	}
	if errors.Is(err, ErrForbidden) {
		return BusinessMember{}, &Error{Code: "MEMBER_OWNER_PROTECTED", Message: "Owner aktif terakhir tidak dapat dinonaktifkan atau diturunkan role-nya."}
	}
	if err != nil {
		return BusinessMember{}, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat mengubah anggota.", Cause: err}
	}
	return item, nil
}
