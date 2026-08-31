package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

var (
	codePattern     = regexp.MustCompile(`^[A-Z0-9][A-Z0-9_-]{1,31}$`)
	currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)
)

type Service struct {
	repo       Repository
	sessionTTL time.Duration
	bcryptCost int
	now        func() time.Time
	random     func(int) ([]byte, error)
}

func NewService(repo Repository, sessionTTL time.Duration, bcryptCost int) *Service {
	return &Service{
		repo:       repo,
		sessionTTL: sessionTTL,
		bcryptCost: bcryptCost,
		now:        time.Now,
		random:     randomBytes,
	}
}

type RegisterInput struct{ Name, Email, Password string }
type LoginInput struct{ Email, Password string }

type AuthResult struct {
	User           User      `json:"user"`
	ActiveBusiness *Business `json:"active_business"`
	CSRFToken      string    `json:"csrf_token"`
}

func (s *Service) Ping(ctx context.Context) error { return s.repo.Ping(ctx) }

func (s *Service) Register(ctx context.Context, in RegisterInput, meta SessionMeta, previousToken string) (AuthResult, SessionCredentials, error) {
	name, email, fields := validateIdentity(in.Name, in.Email, in.Password)
	if len(fields) != 0 {
		return AuthResult{}, SessionCredentials{}, validationError(fields)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), s.bcryptCost)
	if err != nil {
		return AuthResult{}, SessionCredentials{}, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat membuat akun.", Cause: err}
	}
	userCode, err := s.newCode("USR")
	if err != nil {
		return AuthResult{}, SessionCredentials{}, internalRandomError(err)
	}
	newSession, credentials, err := s.newSession(meta)
	if err != nil {
		return AuthResult{}, SessionCredentials{}, internalRandomError(err)
	}
	previousHash := tokenHashOrNil(previousToken)
	record, session, err := s.repo.CreateUserAndSession(ctx, NewUser{
		Code: userCode, Name: name, Email: email, PasswordHash: string(hash),
	}, newSession, previousHash)
	if errors.Is(err, ErrConflict) {
		return AuthResult{}, SessionCredentials{}, &Error{Code: "EMAIL_ALREADY_EXISTS", Message: "Email sudah terdaftar.", Fields: map[string]string{"email": "Email sudah terdaftar."}}
	}
	if err != nil {
		return AuthResult{}, SessionCredentials{}, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat membuat akun.", Cause: err}
	}
	return AuthResult{User: record.User, ActiveBusiness: nil, CSRFToken: credentials.CSRFToken}, withExpiry(credentials, session.ExpiresAt), nil
}

func (s *Service) Login(ctx context.Context, in LoginInput, meta SessionMeta, previousToken string) (AuthResult, SessionCredentials, error) {
	email := strings.ToLower(strings.TrimSpace(in.Email))
	if email == "" || in.Password == "" {
		return AuthResult{}, SessionCredentials{}, &Error{Code: "INVALID_CREDENTIALS", Message: "Email atau password salah."}
	}
	record, err := s.repo.FindUserByEmail(ctx, email)
	if errors.Is(err, ErrNotFound) {
		return AuthResult{}, SessionCredentials{}, &Error{Code: "INVALID_CREDENTIALS", Message: "Email atau password salah."}
	}
	if err != nil {
		return AuthResult{}, SessionCredentials{}, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat masuk.", Cause: err}
	}
	if bcrypt.CompareHashAndPassword([]byte(record.PasswordHash), []byte(in.Password)) != nil {
		return AuthResult{}, SessionCredentials{}, &Error{Code: "INVALID_CREDENTIALS", Message: "Email atau password salah."}
	}
	newSession, credentials, err := s.newSession(meta)
	if err != nil {
		return AuthResult{}, SessionCredentials{}, internalRandomError(err)
	}
	session, err := s.repo.ReplaceSession(ctx, record.ID, newSession, tokenHashOrNil(previousToken))
	if err != nil {
		return AuthResult{}, SessionCredentials{}, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat masuk.", Cause: err}
	}

	var active *Business
	if session.ActiveBusinessID != nil {
		business, businessErr := s.repo.GetBusinessContext(ctx, session.UserID, *session.ActiveBusinessID)
		if businessErr == nil {
			value := business.Business
			active = &value
		}
	}
	return AuthResult{User: record.User, ActiveBusiness: active, CSRFToken: credentials.CSRFToken}, withExpiry(credentials, session.ExpiresAt), nil
}

func (s *Service) Authenticate(ctx context.Context, rawToken string) (Session, error) {
	if rawToken == "" {
		return Session{}, &Error{Code: "UNAUTHENTICATED", Message: "Silakan masuk terlebih dahulu."}
	}
	session, err := s.repo.LoadSession(ctx, hashToken(rawToken))
	if errors.Is(err, ErrNotFound) {
		return Session{}, &Error{Code: "UNAUTHENTICATED", Message: "Sesi tidak valid atau sudah berakhir."}
	}
	if err != nil {
		return Session{}, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat memvalidasi sesi.", Cause: err}
	}
	return session, nil
}

func (s *Service) Logout(ctx context.Context, sessionID string) error {
	if err := s.repo.DeleteSession(ctx, sessionID); err != nil {
		return &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat keluar.", Cause: err}
	}
	return nil
}

func (s *Service) ValidateCSRF(session Session, provided string) bool {
	if provided == "" || len(provided) != len(session.CSRFToken) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(session.CSRFToken)) == 1
}

type CreateBusinessInput struct{ Name, BusinessType, Timezone, Currency string }

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
	if err := s.repo.UpdateBusinessConfiguration(ctx, business.ID, session.UserID, businessType, modules); err != nil {
		return BusinessContext{}, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat menyimpan modul bisnis.", Cause: err}
	}
	business.EnabledModules = modules
	business.BusinessType = businessType
	return business, nil
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
	Name, SKU, Barcode, BaseUnitSymbol                  string
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
		IsStockTracked: tracked,
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
		IsStockTracked: tracked,
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

type OpeningStockInput struct{ ProductCode, LocationCode, Quantity, Reason string }

func (s *Service) CreateOpeningStock(ctx context.Context, session Session, businessID string, in OpeningStockInput) (OpeningStock, error) {
	productCode := strings.ToUpper(strings.TrimSpace(in.ProductCode))
	locationCode := strings.ToUpper(strings.TrimSpace(in.LocationCode))
	if locationCode == "" {
		locationCode = "LOC-DEFAULT"
	}
	fields := map[string]string{}
	if !codePattern.MatchString(productCode) {
		fields["product_code"] = "Kode produk tidak valid."
	}
	if !codePattern.MatchString(locationCode) {
		fields["location_code"] = "Kode lokasi tidak valid."
	}
	quantity, err := normalizeDecimal(in.Quantity, 4, 14, true)
	if err != nil {
		fields["quantity"] = err.Error()
	}
	reason := strings.TrimSpace(in.Reason)
	if utf8.RuneCountInString(reason) > 500 {
		fields["reason"] = "Alasan maksimal 500 karakter."
	}
	if len(fields) != 0 {
		return OpeningStock{}, validationError(fields)
	}
	result, err := s.repo.CreateOpeningStock(ctx, businessID, session.UserID, NewOpeningStock{
		ProductCode: productCode, LocationCode: locationCode,
		Quantity: quantity, Reason: reason,
	})
	if errors.Is(err, ErrNotFound) {
		return OpeningStock{}, &Error{Code: "PRODUCT_OR_LOCATION_NOT_FOUND", Message: "Produk atau lokasi tidak ditemukan pada bisnis aktif."}
	}
	if errors.Is(err, ErrForbidden) {
		return OpeningStock{}, &Error{Code: "STOCK_TRACKING_DISABLED", Message: "Stok tidak dilacak untuk produk ini."}
	}
	if errors.Is(err, ErrConflict) {
		return OpeningStock{}, &Error{Code: "OPENING_STOCK_ALREADY_RECORDED", Message: "Stok awal untuk produk dan lokasi ini sudah pernah dicatat."}
	}
	if err != nil {
		return OpeningStock{}, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat mencatat stok awal.", Cause: err}
	}
	return result, nil
}

func (s *Service) ListInventoryProducts(ctx context.Context, businessID string, search string) ([]InventoryProduct, error) {
	items, err := s.repo.ListInventoryProducts(ctx, businessID, search)
	if err != nil {
		return nil, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat memuat inventaris.", Cause: err}
	}
	return items, nil
}

func (s *Service) ListStockMovements(ctx context.Context, businessID string) ([]StockMovement, error) {
	movements, err := s.repo.ListStockMovements(ctx, businessID)
	if err != nil {
		return nil, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat memuat riwayat pergerakan stok.", Cause: err}
	}
	return movements, nil
}

func (s *Service) CreateStockAdjustment(ctx context.Context, session Session, businessID string, input NewStockAdjustment) (StockAdjustment, error) {
	input.LocationCode = strings.ToUpper(strings.TrimSpace(input.LocationCode))
	if input.LocationCode == "" {
		input.LocationCode = "LOC-DEFAULT"
	}

	if len(input.Items) == 0 {
		return StockAdjustment{}, validationError(map[string]string{"items": "Minimal 1 barang."})
	}

	var validatedItems []NewStockAdjustmentItem
	for i, item := range input.Items {
		item.ProductCode = strings.ToUpper(strings.TrimSpace(item.ProductCode))
		item.Direction = strings.ToUpper(strings.TrimSpace(item.Direction))
		if item.Direction != "IN" && item.Direction != "OUT" {
			return StockAdjustment{}, validationError(map[string]string{fmt.Sprintf("items[%d].direction", i): "Arah harus IN atau OUT."})
		}

		q, err := normalizeDecimal(item.Quantity, 4, 14, true)
		if err != nil {
			return StockAdjustment{}, validationError(map[string]string{fmt.Sprintf("items[%d].quantity", i): err.Error()})
		}
		item.Quantity = q
		item.UnitSymbol = strings.TrimSpace(item.UnitSymbol)
		validatedItems = append(validatedItems, item)
	}
	input.Items = validatedItems

	result, err := s.repo.CreateStockAdjustment(ctx, businessID, session.UserID, input)
	if errors.Is(err, ErrNotFound) {
		return StockAdjustment{}, &Error{Code: "NOT_FOUND", Message: "Produk atau lokasi tidak ditemukan."}
	}
	if err != nil {
		return StockAdjustment{}, &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat membuat penyesuaian stok.", Cause: err}
	}
	return result, nil
}

func (s *Service) CompleteStockAdjustment(ctx context.Context, session Session, businessID, adjustmentNumber string) error {
	err := s.repo.CompleteStockAdjustment(ctx, businessID, session.UserID, adjustmentNumber)
	if err != nil {
		if err.Error() == "stok tidak mencukupi untuk dikeluarkan" {
			return &Error{Code: "INSUFFICIENT_STOCK", Message: err.Error()}
		}
		return &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat menyelesaikan penyesuaian stok.", Cause: err}
	}
	return nil
}

func validateIdentity(rawName, rawEmail, password string) (string, string, map[string]string) {
	name := strings.TrimSpace(rawName)
	email := strings.ToLower(strings.TrimSpace(rawEmail))
	fields := map[string]string{}
	if name == "" || utf8.RuneCountInString(name) > 150 {
		fields["name"] = "Nama wajib diisi dan maksimal 150 karakter."
	}
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email || utf8.RuneCountInString(email) > 254 {
		fields["email"] = "Format email tidak valid."
	}
	if len(password) < 8 {
		fields["password"] = "Password minimal 8 karakter."
	} else if len(password) > 72 {
		fields["password"] = "Password maksimal 72 byte."
	}
	return name, email, fields
}

func (s *Service) newSession(meta SessionMeta) (NewSession, SessionCredentials, error) {
	tokenBytes, err := s.random(32)
	if err != nil {
		return NewSession{}, SessionCredentials{}, err
	}
	csrfBytes, err := s.random(32)
	if err != nil {
		return NewSession{}, SessionCredentials{}, err
	}
	rawToken := base64.RawURLEncoding.EncodeToString(tokenBytes)
	csrfToken := base64.RawURLEncoding.EncodeToString(csrfBytes)
	expires := s.now().UTC().Add(s.sessionTTL)
	return NewSession{
		TokenHash: hashToken(rawToken), CSRFToken: csrfToken, ExpiresAt: expires, Meta: meta,
	}, SessionCredentials{Token: rawToken, CSRFToken: csrfToken, ExpiresAt: expires}, nil
}

func (s *Service) newCode(prefix string) (string, error) {
	random, err := s.random(8)
	if err != nil {
		return "", err
	}
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(random)
	return fmt.Sprintf("%s-%s", prefix, encoded), nil
}

func hashToken(raw string) []byte {
	sum := sha256.Sum256([]byte(raw))
	return sum[:]
}

func tokenHashOrNil(raw string) []byte {
	if raw == "" {
		return nil
	}
	return hashToken(raw)
}

func randomBytes(size int) ([]byte, error) {
	value := make([]byte, size)
	_, err := rand.Read(value)
	return value, err
}

func withExpiry(credentials SessionCredentials, expiresAt time.Time) SessionCredentials {
	credentials.ExpiresAt = expiresAt
	return credentials
}

func internalRandomError(err error) *Error {
	return &Error{Code: "INTERNAL_ERROR", Message: "Tidak dapat membuat kredensial aman.", Cause: err}
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
