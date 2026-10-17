package app

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

type CreateBusinessInput struct {
	Name         string `json:"name"`
	BusinessType string `json:"business_type"`
	Timezone     string `json:"timezone"`
	Currency     string `json:"currency"`
}

func (s *Service) ListBusinesses(ctx context.Context, userID string) ([]Business, error) {
	items, err := s.repo.ListBusinesses(ctx, userID)
	if err != nil {
		return nil, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat memuat bisnis.", Cause: err}
	}
	return items, nil
}

func (s *Service) CreateBusiness(ctx context.Context, session Session, in CreateBusinessInput) (BusinessContext, error) {
	name := strings.TrimSpace(in.Name)
	fields := map[string]string{}
	if name == "" || utf8.RuneCountInString(name) > 150 {
		fields["name"] = "Nama bisnis wajib diisi dan maksimal 150 karakter."
	}
	businessType := strings.ToUpper(strings.TrimSpace(in.BusinessType))
	if businessType == "" {
		businessType = "RETAIL"
	}
	if !oneOf(businessType, "RETAIL", "SERVICE", "ENTERTAINMENT", "OTHER") {
		fields["business_type"] = "Tipe bisnis tidak didukung."
	}
	timezone := strings.TrimSpace(in.Timezone)
	if timezone == "" {
		timezone = "Asia/Jakarta"
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		fields["timezone"] = "Zona waktu tidak valid."
	}
	currency := strings.ToUpper(strings.TrimSpace(in.Currency))
	if currency == "" {
		currency = "IDR"
	}
	if !currencyPattern.MatchString(currency) {
		fields["currency"] = "Mata uang harus berupa kode ISO 3 huruf."
	}
	if len(fields) != 0 {
		return BusinessContext{}, validationError(fields)
	}
	code, err := s.newCode("BUS")
	if err != nil {
		return BusinessContext{}, internalRandomError(err)
	}
	business, err := s.repo.CreateBusiness(ctx, session.UserID, session.ID, NewBusiness{
		Code: code, Name: name, BusinessType: businessType, Timezone: timezone, Currency: currency,
		LocationCode: "LOC-DEFAULT", UnitCode: "UNT-PCS", EnabledModules: DefaultModulesForBusinessType(businessType),
	})
	if err != nil {
		return BusinessContext{}, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat membuat bisnis.", Cause: err}
	}
	return business, nil
}

type UpdateBusinessConfigurationInput struct {
	BusinessType   string
	EnabledModules []string
	// LogoURL memakai nil bila tidak diubah, string kosong untuk menghapus logo.
	LogoURL *string
}

func (s *Service) UpdateBusinessConfiguration(ctx context.Context, session Session, business BusinessContext, in UpdateBusinessConfigurationInput) (BusinessContext, error) {
	if !oneOf(business.Role, "OWNER", "ADMIN") {
		return BusinessContext{}, &Error{Code: "PERMISSION_DENIED", Message: "Anda tidak memiliki izin untuk mengubah modul bisnis."}
	}
	businessType := strings.ToUpper(strings.TrimSpace(in.BusinessType))
	if businessType == "" {
		businessType = business.BusinessType
	}
	if !oneOf(businessType, "RETAIL", "SERVICE", "ENTERTAINMENT", "OTHER") {
		return BusinessContext{}, validationError(map[string]string{"business_type": "Tipe bisnis tidak didukung."})
	}
	modules, err := NormalizeModules(in.EnabledModules)
	if err != nil {
		return BusinessContext{}, validationError(map[string]string{"enabled_modules": err.Error()})
	}
	logoURL, logoFields := normalizeBusinessLogo(in.LogoURL)
	if len(logoFields) != 0 {
		return BusinessContext{}, validationError(logoFields)
	}
	if err := s.repo.UpdateBusinessConfiguration(ctx, business.ID, session.UserID, businessType, modules, logoURL); err != nil {
		return BusinessContext{}, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat menyimpan modul bisnis.", Cause: err}
	}
	business.EnabledModules = modules
	business.BusinessType = businessType
	if logoURL != nil {
		business.LogoURL = *logoURL
	}
	return business, nil
}

// maxBusinessLogoLength mencegah payload outrageous dari data URI.
const maxBusinessLogoLength = 1_500_000

// normalizeBusinessLogo memvalidasi logo toko. Nilai kosong berarti menghapus logo.
func normalizeBusinessLogo(raw *string) (*string, map[string]string) {
	if raw == nil {
		return nil, nil
	}
	value := strings.TrimSpace(*raw)
	if value == "" {
		return &value, nil
	}
	if utf8.RuneCountInString(value) > maxBusinessLogoLength {
		return nil, map[string]string{"logo_url": "Ukuran logo terlalu besar. Gunakan URL atau gambar di bawah 1,5 MB."}
	}
	lowered := strings.ToLower(value)
	switch {
	case strings.HasPrefix(lowered, "data:image/"):
		if !strings.Contains(lowered, ";base64,") {
			return nil, map[string]string{"logo_url": "Logo data URI harus memakai encoding base64."}
		}
	case strings.HasPrefix(lowered, "https://"), strings.HasPrefix(lowered, "http://"):
	default:
		return nil, map[string]string{"logo_url": "Logo harus berupa URL http(s) atau data URI gambar."}
	}
	return &value, nil
}

func (s *Service) BusinessContext(ctx context.Context, session Session) (BusinessContext, error) {
	if session.ActiveBusinessID == nil {
		return BusinessContext{}, &Error{Code: "ACTIVE_BUSINESS_REQUIRED", Message: "Pilih bisnis aktif terlebih dahulu."}
	}
	business, err := s.repo.GetBusinessContext(ctx, session.UserID, *session.ActiveBusinessID)
	if errors.Is(err, ErrForbidden) || errors.Is(err, ErrNotFound) {
		return BusinessContext{}, &Error{Code: "BUSINESS_ACCESS_DENIED", Message: "Anda tidak memiliki akses ke bisnis aktif."}
	}
	if err != nil {
		return BusinessContext{}, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat memuat bisnis aktif.", Cause: err}
	}
	return business, nil
}

func (s *Service) SwitchBusiness(ctx context.Context, session Session, businessCode string) (BusinessContext, error) {
	businessCode = strings.ToUpper(strings.TrimSpace(businessCode))
	if !codePattern.MatchString(businessCode) {
		return BusinessContext{}, validationError(map[string]string{"business_code": "Kode bisnis tidak valid."})
	}
	business, err := s.repo.SwitchBusiness(ctx, session.ID, session.UserID, businessCode)
	if errors.Is(err, ErrForbidden) || errors.Is(err, ErrNotFound) {
		return BusinessContext{}, &Error{Code: "BUSINESS_ACCESS_DENIED", Message: "Anda tidak memiliki akses ke bisnis tersebut."}
	}
	if err != nil {
		return BusinessContext{}, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat mengganti bisnis aktif.", Cause: err}
	}
	return business, nil
}

type CreateProductInput struct {
	Name, SKU, Barcode, BaseUnitSymbol, CategoryCode    string
	DefaultPurchasePrice, DefaultSellingPrice, MinStock string
	IsStockTracked                                      *bool
}

func (s *Service) ListProducts(ctx context.Context, businessID string, search string) ([]Product, error) {
	products, err := s.repo.ListProducts(ctx, businessID, search)
	if err != nil {
		return nil, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat memuat produk.", Cause: err}
	}
	return products, nil
}

func (s *Service) CreateProduct(ctx context.Context, businessID string, in CreateProductInput) (Product, error) {
	name := strings.TrimSpace(in.Name)
	fields := map[string]string{}
	if name == "" || utf8.RuneCountInString(name) > 150 {
		fields["name"] = "Nama produk wajib diisi dan maksimal 150 karakter."
	}
	sku := strings.TrimSpace(in.SKU)
	if utf8.RuneCountInString(sku) > 100 {
		fields["sku"] = "SKU maksimal 100 karakter."
	}
	barcode := strings.TrimSpace(in.Barcode)
	if utf8.RuneCountInString(barcode) > 100 {
		fields["barcode"] = "Barcode maksimal 100 karakter."
	}
	unit := strings.ToUpper(strings.TrimSpace(in.BaseUnitSymbol))
	if unit == "" {
		unit = "PCS"
	}
	categoryCode := strings.ToUpper(strings.TrimSpace(in.CategoryCode))
	if categoryCode != "" && !codePattern.MatchString(categoryCode) {
		fields["category_code"] = "Kode kategori tidak valid."
	}
	purchase, err := decimalOrZero(in.DefaultPurchasePrice, 2, 16)
	if err != nil {
		fields["default_purchase_price"] = err.Error()
	}
	selling, err := decimalOrZero(in.DefaultSellingPrice, 2, 16)
	if err != nil {
		fields["default_selling_price"] = err.Error()
	}
	minStock, err := decimalOrZero(in.MinStock, 4, 14)
	if err != nil {
		fields["min_stock"] = err.Error()
	}
	if len(fields) != 0 {
		return Product{}, validationError(fields)
	}
	tracked := true
	if in.IsStockTracked != nil {
		tracked = *in.IsStockTracked
	}
	product, err := s.repo.CreateProduct(ctx, businessID, NewProduct{
		Name: name, SKU: sku, Barcode: barcode, BaseUnitSymbol: unit,
		DefaultPurchasePrice: purchase, DefaultSellingPrice: selling, MinStock: minStock,
		IsStockTracked: tracked, CategoryCode: categoryCode,
	})
	if errors.Is(err, ErrNotFound) {
		return Product{}, validationError(map[string]string{"base_unit_symbol": "Satuan tidak ditemukan."})
	}
	if errors.Is(err, ErrConflict) {
		return Product{}, &Error{Code: "PRODUCT_CONFLICT", Message: "SKU atau barcode sudah digunakan dalam bisnis ini."}
	}
	if err != nil {
		return Product{}, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat membuat produk.", Cause: err}
	}
	return product, nil
}

func (s *Service) UpdateProduct(ctx context.Context, businessID string, code string, in CreateProductInput) (Product, error) {
	name := strings.TrimSpace(in.Name)
	fields := map[string]string{}
	if name == "" || utf8.RuneCountInString(name) > 150 {
		fields["name"] = "Nama produk wajib diisi dan maksimal 150 karakter."
	}
	sku := strings.TrimSpace(in.SKU)
	if utf8.RuneCountInString(sku) > 100 {
		fields["sku"] = "SKU maksimal 100 karakter."
	}
	barcode := strings.TrimSpace(in.Barcode)
	if utf8.RuneCountInString(barcode) > 100 {
		fields["barcode"] = "Barcode maksimal 100 karakter."
	}
	unit := strings.ToUpper(strings.TrimSpace(in.BaseUnitSymbol))
	if unit == "" {
		unit = "PCS"
	}
	categoryCode := strings.ToUpper(strings.TrimSpace(in.CategoryCode))
	if categoryCode != "" && !codePattern.MatchString(categoryCode) {
		fields["category_code"] = "Kode kategori tidak valid."
	}
	purchase, err := decimalOrZero(in.DefaultPurchasePrice, 2, 16)
	if err != nil {
		fields["default_purchase_price"] = err.Error()
	}
	selling, err := decimalOrZero(in.DefaultSellingPrice, 2, 16)
	if err != nil {
		fields["default_selling_price"] = err.Error()
	}
	minStock, err := decimalOrZero(in.MinStock, 4, 14)
	if err != nil {
		fields["min_stock"] = err.Error()
	}
	if len(fields) != 0 {
		return Product{}, validationError(fields)
	}
	tracked := true
	if in.IsStockTracked != nil {
		tracked = *in.IsStockTracked
	}
	product, err := s.repo.UpdateProduct(ctx, businessID, code, NewProduct{
		Name: name, SKU: sku, Barcode: barcode, BaseUnitSymbol: unit,
		DefaultPurchasePrice: purchase, DefaultSellingPrice: selling, MinStock: minStock,
		IsStockTracked: tracked, CategoryCode: categoryCode,
	})
	if errors.Is(err, ErrNotFound) {
		return Product{}, &Error{Code: "PRODUCT_NOT_FOUND", Message: "Produk tidak ditemukan atau satuan tidak valid."}
	}
	if errors.Is(err, ErrConflict) {
		return Product{}, &Error{Code: "PRODUCT_CONFLICT", Message: "SKU atau barcode sudah digunakan dalam bisnis ini."}
	}
	if err != nil {
		return Product{}, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat mengubah produk.", Cause: err}
	}
	return product, nil
}

func (s *Service) DeleteProduct(ctx context.Context, session Session, businessID string, code string) error {
	err := s.repo.DeleteProduct(ctx, businessID, code, session.UserID)
	if errors.Is(err, ErrNotFound) {
		return &Error{Code: "PRODUCT_NOT_FOUND", Message: "Produk tidak ditemukan."}
	}
	if errors.Is(err, ErrConflict) {
		return &Error{Code: "PRODUCT_HAS_HISTORY", Message: "Produk tidak bisa dihapus karena sudah memiliki riwayat stok atau transaksi."}
	}
	if err != nil {
		return &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat menghapus produk.", Cause: err}
	}
	return nil
}

func decimalOrZero(raw string, scale, maxIntegerDigits int) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "0", nil
	}
	return normalizeDecimal(raw, scale, maxIntegerDigits, false)
}

func oneOf(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if value == candidate {
			return true
		}
	}
	return false
}
