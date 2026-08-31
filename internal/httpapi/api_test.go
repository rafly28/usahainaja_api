package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"usahainaja/backend/internal/app"
)

type apiRepositoryStub struct {
	session  app.Session
	business app.BusinessContext
}

func (s *apiRepositoryStub) Ping(context.Context) error { return nil }
func (s *apiRepositoryStub) CreateUserAndSession(context.Context, app.NewUser, app.NewSession, []byte) (app.UserRecord, app.Session, error) {
	return app.UserRecord{}, app.Session{}, nil
}
func (s *apiRepositoryStub) FindUserByEmail(context.Context, string) (app.UserRecord, error) {
	return app.UserRecord{}, app.ErrNotFound
}
func (s *apiRepositoryStub) ReplaceSession(context.Context, string, app.NewSession, []byte) (app.Session, error) {
	return app.Session{}, nil
}
func (s *apiRepositoryStub) LoadSession(context.Context, []byte) (app.Session, error) {
	return s.session, nil
}
func (s *apiRepositoryStub) DeleteSession(context.Context, string) error { return nil }
func (s *apiRepositoryStub) ListBusinesses(context.Context, string) ([]app.Business, error) {
	return []app.Business{}, nil
}
func (s *apiRepositoryStub) CreateBusiness(context.Context, string, string, app.NewBusiness) (app.BusinessContext, error) {
	return s.business, nil
}
func (s *apiRepositoryStub) GetBusinessContext(context.Context, string, string) (app.BusinessContext, error) {
	return s.business, nil
}
func (s *apiRepositoryStub) SwitchBusiness(context.Context, string, string, string) (app.BusinessContext, error) {
	return s.business, nil
}
func (s *apiRepositoryStub) UpdateBusinessConfiguration(context.Context, string, string, string, []string) error {
	return nil
}
func (r *apiRepositoryStub) ListProducts(ctx context.Context, businessID string, search string) ([]app.Product, error) {
	return []app.Product{}, nil
}
func (r *apiRepositoryStub) CreateProduct(ctx context.Context, businessID string, input app.NewProduct) (app.Product, error) {
	return app.Product{}, nil
}
func (r *apiRepositoryStub) UpdateProduct(ctx context.Context, businessID string, code string, input app.NewProduct) (app.Product, error) {
	return app.Product{}, nil
}
func (r *apiRepositoryStub) DeleteProduct(ctx context.Context, businessID, code, userID string) error {
	return nil
}
func (s *apiRepositoryStub) CreateOpeningStock(context.Context, string, string, app.NewOpeningStock) (app.OpeningStock, error) {
	return app.OpeningStock{}, nil
}
func (r *apiRepositoryStub) ListInventoryProducts(ctx context.Context, businessID string, search string) ([]app.InventoryProduct, error) {
	return nil, nil
}
func (s *apiRepositoryStub) ListStockMovements(ctx context.Context, businessID string) ([]app.StockMovement, error) {
	return nil, nil
}
func (s *apiRepositoryStub) CreateStockAdjustment(ctx context.Context, businessID, userID string, input app.NewStockAdjustment) (app.StockAdjustment, error) {
	return app.StockAdjustment{}, nil
}
func (s *apiRepositoryStub) CompleteStockAdjustment(ctx context.Context, businessID, userID, adjustmentNumber string) error {
	return nil
}
func (s *apiRepositoryStub) ListContacts(context.Context, string) ([]app.Contact, error) {
	return nil, nil
}
func (s *apiRepositoryStub) CreateContact(context.Context, string, app.NewContact) (app.Contact, error) {
	return app.Contact{}, nil
}
func (s *apiRepositoryStub) ListCashAccounts(context.Context, string) ([]app.CashAccount, error) {
	return nil, nil
}
func (s *apiRepositoryStub) CreateCashAccount(context.Context, string, app.NewCashAccount) (app.CashAccount, error) {
	return app.CashAccount{}, nil
}
func (s *apiRepositoryStub) ListSales(context.Context, string) ([]app.Sale, error) {
	return nil, nil
}
func (s *apiRepositoryStub) CreateSale(context.Context, string, string, app.NewSale) (app.Sale, error) {
	return app.Sale{}, nil
}
func (s *apiRepositoryStub) ListPurchases(context.Context, string) ([]app.Purchase, error) {
	return nil, nil
}
func (s *apiRepositoryStub) CreatePurchase(context.Context, string, string, app.NewPurchase) (app.Purchase, error) {
	return app.Purchase{}, nil
}

func (s *apiRepositoryStub) ReceivePurchase(ctx context.Context, businessID, purchaseNumber, userID string) error {
	return nil
}

func (s *apiRepositoryStub) RecordPurchasePayment(ctx context.Context, businessID, purchaseNumber, userID string, in app.PaymentInput) (app.Payment, error) {
	return app.Payment{}, nil
}

func testHandler(role string) http.Handler {
	businessID := "business-id"
	repo := &apiRepositoryStub{
		session: app.Session{
			ID: "session-id", UserID: "user-id", User: app.User{Code: "USR-1", Name: "Budi", Email: "budi@example.com"},
			ActiveBusinessID: &businessID, CSRFToken: "csrf-token", ExpiresAt: time.Now().Add(time.Hour),
		},
		business: app.BusinessContext{ID: businessID, Role: role, Business: app.Business{Code: "BUS-1", Name: "Toko", Role: role, EnabledModules: []string{app.ModuleCatalog, app.ModuleInventory, app.ModuleSales, app.ModulePurchase, app.ModuleFinance}}},
	}
	return New(app.NewService(repo, time.Hour, bcrypt.MinCost), "session", false)
}

func TestCollectionRoutesAcceptCanonicalPathsWithoutTrailingSlash(t *testing.T) {
	handler := testHandler("OWNER")
	for _, path := range []string{"/api/businesses", "/api/products"} {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, path, nil)
			request.AddCookie(&http.Cookie{Name: "session", Value: "token"})
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
			var envelope struct {
				Success bool `json:"success"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil || !envelope.Success {
				t.Fatalf("invalid success envelope: %s (%v)", response.Body.String(), err)
			}
		})
	}
}

func TestProductWriteRequiresCSRFAndPrivilegedRole(t *testing.T) {
	handler := testHandler("VIEWER")
	request := httptest.NewRequest(http.MethodPost, "/api/products", strings.NewReader(`{"name":"Apel"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", "csrf-token")
	request.AddCookie(&http.Cookie{Name: "session", Value: "token"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestJSONMutationRequiresApplicationJSON(t *testing.T) {
	handler := testHandler("OWNER")
	request := httptest.NewRequest(http.MethodPost, "/api/products", strings.NewReader(`{"name":"Apel"}`))
	request.Header.Set("X-CSRF-Token", "csrf-token")
	request.AddCookie(&http.Cookie{Name: "session", Value: "token"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestModuleDisabledRejectsProtectedEndpoint(t *testing.T) {
	businessID := "business-id"
	repo := &apiRepositoryStub{
		session:  app.Session{ID: "session-id", UserID: "user-id", ActiveBusinessID: &businessID, CSRFToken: "csrf-token", ExpiresAt: time.Now().Add(time.Hour)},
		business: app.BusinessContext{ID: businessID, Role: "OWNER", Business: app.Business{Code: "BUS-1", Name: "Jasa", BusinessType: "SERVICE", Role: "OWNER", EnabledModules: []string{app.ModuleBooking, app.ModuleFinance}}},
	}
	handler := New(app.NewService(repo, time.Hour, bcrypt.MinCost), "session", false)
	request := httptest.NewRequest(http.MethodPost, "/api/products", strings.NewReader(`{"name":"Apel"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", "csrf-token")
	request.AddCookie(&http.Cookie{Name: "session", Value: "token"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestOwnerCanUpdateBusinessConfiguration(t *testing.T) {
	handler := testHandler("OWNER")
	request := httptest.NewRequest(http.MethodPut, "/api/businesses/current/configuration", strings.NewReader(`{"business_type":"SERVICE","enabled_modules":["BOOKING","FINANCE"]}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", "csrf-token")
	request.AddCookie(&http.Cookie{Name: "session", Value: "token"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var envelope struct {
		Data struct {
			BusinessType   string   `json:"business_type"`
			EnabledModules []string `json:"enabled_modules"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Data.BusinessType != "SERVICE" || len(envelope.Data.EnabledModules) != 2 {
		t.Fatalf("configuration response = %#v", envelope.Data)
	}
}
