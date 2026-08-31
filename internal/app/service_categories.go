package app

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"
)

func (s *Service) ListCategories(ctx context.Context, businessID, categoryType string) ([]Category, error) {
	categoryType = strings.ToUpper(strings.TrimSpace(categoryType))
	if categoryType != "" && !oneOf(categoryType, "PRODUCT", "SERVICE", "ASSET", "EXPENSE") {
		return nil, validationError(map[string]string{"category_type": "Tipe kategori tidak didukung."})
	}
	items, err := s.repo.ListCategories(ctx, businessID, categoryType)
	if err != nil {
		return nil, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat memuat kategori.", Cause: err}
	}
	return items, nil
}

type CreateCategoryInput struct {
	Name         string `json:"name"`
	CategoryType string `json:"category_type"`
	ParentCode   string `json:"parent_code"`
}

func (s *Service) CreateCategory(ctx context.Context, session Session, business BusinessContext, in CreateCategoryInput) (Category, error) {
	if !oneOf(business.Role, "OWNER", "ADMIN") {
		return Category{}, &Error{Code: "PERMISSION_DENIED", Message: "Anda tidak memiliki izin untuk mengelola kategori."}
	}
	input, err := normalizeCategoryInput(in)
	if err != nil {
		return Category{}, err
	}
	item, err := s.repo.CreateCategory(ctx, business.ID, session.UserID, input)
	if errors.Is(err, ErrNotFound) {
		return Category{}, validationError(map[string]string{"parent_code": "Kategori induk tidak ditemukan atau berbeda tipe."})
	}
	if errors.Is(err, ErrConflict) {
		return Category{}, &Error{Code: "CATEGORY_CONFLICT", Message: "Kategori dengan nama tersebut sudah ada."}
	}
	if err != nil {
		return Category{}, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat membuat kategori.", Cause: err}
	}
	return item, nil
}

type UpdateCategoryInput struct {
	Name         string `json:"name"`
	CategoryType string `json:"category_type"`
	ParentCode   string `json:"parent_code"`
	Status       string `json:"status"`
}

func (s *Service) UpdateCategory(ctx context.Context, session Session, business BusinessContext, code string, in UpdateCategoryInput) (Category, error) {
	if !oneOf(business.Role, "OWNER", "ADMIN") {
		return Category{}, &Error{Code: "PERMISSION_DENIED", Message: "Anda tidak memiliki izin untuk mengelola kategori."}
	}
	code = strings.ToUpper(strings.TrimSpace(code))
	if !codePattern.MatchString(code) {
		return Category{}, validationError(map[string]string{"code": "Kode kategori tidak valid."})
	}
	input, err := normalizeCategoryInput(CreateCategoryInput{Name: in.Name, ParentCode: in.ParentCode, CategoryType: in.CategoryType})
	if err != nil {
		return Category{}, err
	}
	status := strings.ToUpper(strings.TrimSpace(in.Status))
	if status == "" {
		status = "ACTIVE"
	}
	if !oneOf(status, "ACTIVE", "INACTIVE") {
		return Category{}, validationError(map[string]string{"status": "Status kategori tidak valid."})
	}
	item, err := s.repo.UpdateCategory(ctx, business.ID, session.UserID, code, status, input)
	if errors.Is(err, ErrNotFound) {
		return Category{}, &Error{Code: "CATEGORY_NOT_FOUND", Message: "Kategori tidak ditemukan atau induk tidak valid."}
	}
	if errors.Is(err, ErrConflict) {
		return Category{}, &Error{Code: "CATEGORY_CONFLICT", Message: "Kategori dengan nama tersebut sudah ada."}
	}
	if err != nil {
		return Category{}, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat mengubah kategori.", Cause: err}
	}
	return item, nil
}

func normalizeCategoryInput(in CreateCategoryInput) (NewCategory, error) {
	name := strings.TrimSpace(in.Name)
	categoryType := strings.ToUpper(strings.TrimSpace(in.CategoryType))
	parentCode := strings.ToUpper(strings.TrimSpace(in.ParentCode))
	fields := map[string]string{}
	if name == "" || utf8.RuneCountInString(name) > 150 {
		fields["name"] = "Nama kategori wajib diisi dan maksimal 150 karakter."
	}
	if !oneOf(categoryType, "PRODUCT", "SERVICE", "ASSET", "EXPENSE") {
		fields["category_type"] = "Tipe kategori tidak didukung."
	}
	if parentCode != "" && !codePattern.MatchString(parentCode) {
		fields["parent_code"] = "Kode kategori induk tidak valid."
	}
	if len(fields) != 0 {
		return NewCategory{}, validationError(fields)
	}
	return NewCategory{Name: name, CategoryType: categoryType, ParentCode: parentCode}, nil
}
