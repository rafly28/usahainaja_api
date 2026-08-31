package app

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type repositoryStub struct {
	createdUser                 NewUser
	createdSession              NewSession
	previousTokenHash           []byte
	ListProductsFunc            func(ctx context.Context, businessID string) ([]Product, error)
	CreateProductFunc           func(ctx context.Context, businessID string, input NewProduct) (Product, error)
	UpdateProductFunc           func(ctx context.Context, businessID, code string, input NewProduct) (Product, error)
	DeleteProductFunc           func(ctx context.Context, businessID, code string) error
	CreateOpeningStockFunc      func(ctx context.Context, businessID, userID string, input NewOpeningStock) (OpeningStock, error)
	ListInventoryProductsFunc   func(ctx context.Context, businessID string) ([]InventoryProduct, error)
	ListStockMovementsFunc      func(ctx context.Context, businessID string) ([]StockMovement, error)
	CreateStockAdjustmentFunc   func(ctx context.Context, businessID, userID string, input NewStockAdjustment) (StockAdjustment, error)
	CompleteStockAdjustmentFunc func(ctx context.Context, businessID, userID, adjustmentNumber string) error
	UpdateBusinessConfigFunc    func(ctx context.Context, businessID, userID, businessType string, modules []string) error
	replacedModules             []string
	replacedBusinessID          string
	replacedActorID             string
	openingStockErr             error
}

func (s *repositoryStub) Ping(context.Context) error { return nil }
func (s *repositoryStub) CreateUserAndSession(_ context.Context, user NewUser, session NewSession, previous []byte) (UserRecord, Session, error) {
	s.createdUser, s.createdSession, s.previousTokenHash = user, session, previous
	record := UserRecord{ID: "user-id", User: User{Code: user.Code, Name: user.Name, Email: user.Email}, PasswordHash: user.PasswordHash}
	return record, Session{ID: "session-id", UserID: record.ID, User: record.User, CSRFToken: session.CSRFToken, ExpiresAt: session.ExpiresAt}, nil
}
func (s *repositoryStub) FindUserByEmail(context.Context, string) (UserRecord, error) {
	return UserRecord{}, ErrNotFound
}
func (s *repositoryStub) ReplaceSession(context.Context, string, NewSession, []byte) (Session, error) {
	return Session{}, nil
}
func (s *repositoryStub) LoadSession(context.Context, []byte) (Session, error) {
	return Session{}, ErrNotFound
}
func (s *repositoryStub) DeleteSession(context.Context, string) error { return nil }
func (s *repositoryStub) ListBusinesses(context.Context, string) ([]Business, error) {
	return []Business{}, nil
}
func (s *repositoryStub) CreateBusiness(context.Context, string, string, NewBusiness) (BusinessContext, error) {
	return BusinessContext{}, nil
}
func (s *repositoryStub) GetBusinessContext(context.Context, string, string) (BusinessContext, error) {
	return BusinessContext{}, nil
}
func (s *repositoryStub) SwitchBusiness(context.Context, string, string, string) (BusinessContext, error) {
	return BusinessContext{}, nil
}

func (s *repositoryStub) UpdateBusinessConfiguration(ctx context.Context, businessID, actorID, businessType string, modules []string) error {
	if s.UpdateBusinessConfigFunc != nil {
		return s.UpdateBusinessConfigFunc(ctx, businessID, actorID, businessType, modules)
	}
	return nil
}
func (s *repositoryStub) ListCategories(context.Context, string, string) ([]Category, error) {
	return nil, nil
}
func (s *repositoryStub) CreateCategory(context.Context, string, string, NewCategory) (Category, error) {
	return Category{}, nil
}
func (s *repositoryStub) UpdateCategory(context.Context, string, string, string, string, NewCategory) (Category, error) {
	return Category{}, nil
}
func (s *repositoryStub) ListUnits(context.Context, string) ([]Unit, error) { return nil, nil }
func (s *repositoryStub) CreateUnit(context.Context, string, string, NewUnit) (Unit, error) {
	return Unit{}, nil
}
func (s *repositoryStub) UpdateUnit(context.Context, string, string, string, string, NewUnit) (Unit, error) {
	return Unit{}, nil
}
func (s *repositoryStub) ListLocations(context.Context, string) ([]Location, error) { return nil, nil }
func (s *repositoryStub) CreateLocation(context.Context, string, string, NewLocation) (Location, error) {
	return Location{}, nil
}
func (s *repositoryStub) UpdateLocation(context.Context, string, string, string, string, NewLocation) (Location, error) {
	return Location{}, nil
}
func (s *repositoryStub) ListParties(context.Context, string) ([]Party, error) { return nil, nil }
func (s *repositoryStub) CreateParty(context.Context, string, string, NewParty) (Party, error) {
	return Party{}, nil
}
func (r *repositoryStub) ListProducts(ctx context.Context, businessID string, search string) ([]Product, error) {
	if r.ListProductsFunc != nil {
		return r.ListProductsFunc(ctx, businessID)
	}
	return []Product{}, nil
}
func (r *repositoryStub) CreateProduct(ctx context.Context, businessID string, input NewProduct) (Product, error) {
	if r.CreateProductFunc != nil {
		return r.CreateProductFunc(ctx, businessID, input)
	}
	return Product{}, nil
}
func (r *repositoryStub) UpdateProduct(ctx context.Context, businessID string, code string, input NewProduct) (Product, error) {
	if r.UpdateProductFunc != nil {
		return r.UpdateProductFunc(ctx, businessID, code, input)
	}
	return Product{}, nil
}
func (r *repositoryStub) DeleteProduct(ctx context.Context, businessID, code, userID string) error {
	if r.DeleteProductFunc != nil {
		return r.DeleteProductFunc(ctx, businessID, code)
	}
	return nil
}
func (s *repositoryStub) CreateOpeningStock(ctx context.Context, businessID, userID string, input NewOpeningStock) (OpeningStock, error) {
	if s.CreateOpeningStockFunc != nil {
		return s.CreateOpeningStockFunc(ctx, businessID, userID, input)
	}
	return OpeningStock{}, s.openingStockErr
}
func (r *repositoryStub) ListInventoryProducts(ctx context.Context, businessID string, search string) ([]InventoryProduct, error) {
	if r.ListInventoryProductsFunc != nil {
		return r.ListInventoryProductsFunc(ctx, businessID)
	}
	return []InventoryProduct{}, nil
}
func (s *repositoryStub) ListStockMovements(ctx context.Context, businessID string) ([]StockMovement, error) {
	return s.ListStockMovementsFunc(ctx, businessID)
}
func (s *repositoryStub) CreateStockAdjustment(ctx context.Context, businessID, userID string, input NewStockAdjustment) (StockAdjustment, error) {
	return s.CreateStockAdjustmentFunc(ctx, businessID, userID, input)
}
func (s *repositoryStub) CompleteStockAdjustment(ctx context.Context, businessID, userID, adjustmentNumber string) error {
	return s.CompleteStockAdjustmentFunc(ctx, businessID, userID, adjustmentNumber)
}
func (s *repositoryStub) ListContacts(context.Context, string) ([]Contact, error) {
	return nil, nil
}
func (s *repositoryStub) CreateContact(context.Context, string, NewContact) (Contact, error) {
	return Contact{}, nil
}
func (s *repositoryStub) ListCashAccounts(context.Context, string) ([]CashAccount, error) {
	return nil, nil
}
func (s *repositoryStub) CreateCashAccount(context.Context, string, NewCashAccount) (CashAccount, error) {
	return CashAccount{}, nil
}
func (s *repositoryStub) ListSales(context.Context, string) ([]Sale, error) {
	return nil, nil
}
func (s *repositoryStub) CreateSale(context.Context, string, string, NewSale) (Sale, error) {
	return Sale{}, nil
}
func (s *repositoryStub) ListPurchases(context.Context, string) ([]Purchase, error) {
	return nil, nil
}
func (s *repositoryStub) CreatePurchase(context.Context, string, string, NewPurchase) (Purchase, error) {
	return Purchase{}, nil
}

func (s *repositoryStub) ReceivePurchase(ctx context.Context, businessID, purchaseNumber, userID string) error {
	return nil
}

func (s *repositoryStub) RecordPurchasePayment(ctx context.Context, businessID, purchaseNumber, userID string, in PaymentInput) (Payment, error) {
	return Payment{}, nil
}

func TestRegisterNormalizesIdentityHashesPasswordAndRotatesSession(t *testing.T) {
	repo := &repositoryStub{}
	service := NewService(repo, time.Hour, bcrypt.MinCost)
	service.now = func() time.Time { return time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC) }
	service.random = func(size int) ([]byte, error) {
		value := make([]byte, size)
		for i := range value {
			value[i] = byte(i + 1)
		}
		return value, nil
	}

	result, credentials, err := service.Register(context.Background(), RegisterInput{
		Name: "  Budi  ", Email: "  BUDI@Example.COM ", Password: "rahasia-kuat",
	}, SessionMeta{}, "old-session-token")
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if result.User.Name != "Budi" || result.User.Email != "budi@example.com" {
		t.Fatalf("identity was not normalized: %#v", result.User)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(repo.createdUser.PasswordHash), []byte("rahasia-kuat")); err != nil {
		t.Fatalf("password was not bcrypt hashed: %v", err)
	}
	if string(repo.previousTokenHash) != string(hashToken("old-session-token")) {
		t.Fatal("previous session token was not hashed for rotation")
	}
	if credentials.Token == "" || result.CSRFToken == "" || result.CSRFToken != credentials.CSRFToken {
		t.Fatalf("credentials missing from auth result: %#v %#v", result, credentials)
	}
	if !credentials.ExpiresAt.Equal(service.now().Add(time.Hour)) {
		t.Fatalf("unexpected expiry: %v", credentials.ExpiresAt)
	}
}

func TestOpeningStockConflictHasStableDomainError(t *testing.T) {
	repo := &repositoryStub{openingStockErr: ErrConflict}
	service := NewService(repo, time.Hour, bcrypt.MinCost)
	_, err := service.CreateOpeningStock(context.Background(), Session{UserID: "user-id"}, "business-id", OpeningStockInput{
		ProductCode: "PRD-000001", LocationCode: "LOC-DEFAULT", Quantity: "2.5",
	})
	var appErr *Error
	if !errors.As(err, &appErr) || appErr.Code != "OPENING_STOCK_ALREADY_RECORDED" {
		t.Fatalf("unexpected error: %#v", err)
	}
}

func TestNormalizeDecimalPreservesPrecision(t *testing.T) {
	value, err := normalizeDecimal("00012.3400", 4, 14, true)
	if err != nil || value != "12.34" {
		t.Fatalf("normalizeDecimal() = %q, %v", value, err)
	}
	if _, err := normalizeDecimal("1.00001", 4, 14, true); err == nil {
		t.Fatal("normalizeDecimal() should reject more than four fractional digits")
	}
	if _, err := normalizeDecimal("0", 4, 14, true); err == nil {
		t.Fatal("normalizeDecimal() should reject zero when positive is required")
	}
}

func TestValidateCSRF(t *testing.T) {
	service := NewService(&repositoryStub{}, time.Hour, bcrypt.MinCost)
	session := Session{CSRFToken: "known-token"}
	if !service.ValidateCSRF(session, "known-token") {
		t.Fatal("expected matching token to pass")
	}
	if service.ValidateCSRF(session, "wrong-token") {
		t.Fatal("expected a different token to fail")
	}
}

func TestBusinessProfilesHaveExpectedInitialModules(t *testing.T) {
	tests := map[string][]string{
		"RETAIL":        {ModuleCatalog, ModuleInventory, ModuleSales, ModulePurchase, ModuleFinance, ModuleReporting},
		"SERVICE":       {ModuleBooking, ModuleFinance, ModuleReporting},
		"ENTERTAINMENT": {ModuleBooking, ModuleFinance, ModuleReporting},
		"OTHER":         {},
	}
	for businessType, want := range tests {
		t.Run(businessType, func(t *testing.T) {
			got := DefaultModulesForBusinessType(businessType)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("DefaultModulesForBusinessType(%q) = %#v, want %#v", businessType, got, want)
			}
		})
	}
}

func TestNormalizeModulesRejectsUnknownAndUsesCanonicalOrder(t *testing.T) {
	got, err := NormalizeModules([]string{" sales ", "INVENTORY", "SALES"})
	if err != nil {
		t.Fatalf("NormalizeModules() error = %v", err)
	}
	want := []string{ModuleInventory, ModuleSales}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NormalizeModules() = %#v, want %#v", got, want)
	}
	if _, err := NormalizeModules([]string{"UNSUPPORTED"}); err == nil {
		t.Fatal("NormalizeModules() should reject unknown module")
	}
}

func TestUpdateBusinessModulesUsesValidatedModuleConfiguration(t *testing.T) {
	repo := &repositoryStub{}
	repo.UpdateBusinessConfigFunc = func(_ context.Context, businessID, actorID, businessType string, modules []string) error {
		repo.replacedBusinessID, repo.replacedActorID = businessID, actorID
		repo.replacedModules = append([]string(nil), modules...)
		return nil
	}
	service := NewService(repo, time.Hour, bcrypt.MinCost)
	business, err := service.UpdateBusinessConfiguration(context.Background(), Session{UserID: "owner-id"}, BusinessContext{
		ID: "business-id", Role: "OWNER", Business: Business{BusinessType: "RETAIL", EnabledModules: []string{ModuleCatalog}},
	}, UpdateBusinessConfigurationInput{BusinessType: "service", EnabledModules: []string{"finance", "sales", "FINANCE"}})
	if err != nil {
		t.Fatalf("UpdateBusinessModules() error = %v", err)
	}
	want := []string{ModuleSales, ModuleFinance}
	if !reflect.DeepEqual(business.EnabledModules, want) || !reflect.DeepEqual(repo.replacedModules, want) {
		t.Fatalf("updated modules = %#v, persisted = %#v, want %#v", business.EnabledModules, repo.replacedModules, want)
	}
	if repo.replacedBusinessID != "business-id" || repo.replacedActorID != "owner-id" {
		t.Fatalf("persistence scope = (%q, %q)", repo.replacedBusinessID, repo.replacedActorID)
	}
	if business.BusinessType != "SERVICE" {
		t.Fatalf("business type = %q, want SERVICE", business.BusinessType)
	}
	_, err = service.UpdateBusinessConfiguration(context.Background(), Session{UserID: "viewer-id"}, BusinessContext{ID: "business-id", Role: "VIEWER"}, UpdateBusinessConfigurationInput{})
	var appErr *Error
	if !errors.As(err, &appErr) || appErr.Code != "PERMISSION_DENIED" {
		t.Fatalf("viewer update error = %#v", err)
	}
}

func TestNormalizeCategoryInput(t *testing.T) {
	category, err := normalizeCategoryInput(CreateCategoryInput{Name: "  Minuman ", CategoryType: "product", ParentCode: "cat-000001"})
	if err != nil {
		t.Fatalf("normalizeCategoryInput() error = %v", err)
	}
	if category.Name != "Minuman" || category.CategoryType != "PRODUCT" || category.ParentCode != "CAT-000001" {
		t.Fatalf("normalized category = %#v", category)
	}
	if _, err := normalizeCategoryInput(CreateCategoryInput{Name: "", CategoryType: "unknown"}); err == nil {
		t.Fatal("expected invalid category input to be rejected")
	}
}

func TestNormalizeUnitInput(t *testing.T) {
	unit, err := normalizeUnitInput(" Kilogram ", " kg ", "weight")
	if err != nil {
		t.Fatalf("normalizeUnitInput() error = %v", err)
	}
	if unit.Name != "Kilogram" || unit.Symbol != "KG" || unit.UnitType != "WEIGHT" {
		t.Fatalf("normalized unit = %#v", unit)
	}
	if _, err := normalizeUnitInput("", "", "invalid"); err == nil {
		t.Fatal("expected invalid unit input")
	}
}
