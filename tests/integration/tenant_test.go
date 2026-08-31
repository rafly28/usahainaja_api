//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"usahainaja/backend/db/migrations"
	"usahainaja/backend/internal/app"
	"usahainaja/backend/internal/httpapi"
	"usahainaja/backend/internal/postgres"
)

func TestMigrationIdempotency(t *testing.T) {
	// Scenario 1: Run migrations twice and ensure idempotency.
	pool, _, cleanup := setupTestDB(t)
	defer cleanup()

	// It was already run once in setupTestDB. Let's run it again.
	err := migrations.Up(context.Background(), pool)
	if err != nil {
		t.Fatalf("Expected nil error when running migrations again, got %v", err)
	}
}

func setupApp(t *testing.T) (*pgxpool.Pool, http.Handler) {
	pool, _, cleanup := setupTestDB(t)
	t.Cleanup(cleanup)

	repo := postgres.New(pool)
	service := app.NewService(repo, time.Hour, 10) // low bcrypt cost for tests
	handler := httpapi.New(service, "test_session", false)
	return pool, handler
}

func TestAuthEndpoints(t *testing.T) {
	_, handler := setupApp(t)
	server := httptest.NewServer(handler)
	defer server.Close()

	client := server.Client()

	// Wait, we need a cookie jar for sessions.
	jar, _ := cookiejar.New(nil)
	client.Jar = jar

	// 1. Register
	registerBody := strings.NewReader(`{"name":"Integration User","email":"test@integration.com","password":"password123"}`)
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/auth/register", registerBody)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("failed to register: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 200/201 OK for register, got %d", resp.StatusCode)
	}

	// 2. Get CSRF token
	req, _ = http.NewRequest(http.MethodGet, server.URL+"/api/auth/csrf", nil)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("failed to get csrf: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for /api/auth/csrf, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var csrfResp struct {
		Data struct {
			CSRFToken string `json:"csrf_token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &csrfResp); err != nil {
		t.Fatalf("failed to parse csrf response: %v", err)
	}
	csrfToken := csrfResp.Data.CSRFToken
	if csrfToken == "" {
		t.Fatalf("expected non-empty csrf token. body: %s", string(body))
	}

	// 3. Logout
	req, _ = http.NewRequest(http.MethodPost, server.URL+"/api/auth/logout", nil)
	req.Header.Set("X-CSRF-Token", csrfToken)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("failed to logout: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for logout, got %d", resp.StatusCode)
	}
}

func loginAndGetCSRF(t *testing.T, client *http.Client, url string, email, password string) string {
	t.Helper()

	// 1. Get CSRF token
	req, _ := http.NewRequest(http.MethodGet, url+"/api/auth/csrf", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("failed to get csrf: %v", err)
	}
	defer resp.Body.Close()
	var csrfResp struct {
		Data struct {
			CSRFToken string `json:"csrf_token"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&csrfResp); err != nil {
		t.Fatalf("failed to parse csrf response: %v", err)
	}
	csrfToken := csrfResp.Data.CSRFToken

	// 2. Login
	if email != "" && password != "" {
		loginBody := strings.NewReader(`{"email":"` + email + `","password":"` + password + `"}`)
		req, _ = http.NewRequest(http.MethodPost, url+"/api/auth/login", loginBody)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", csrfToken)
		resp, err = client.Do(req)
		if err != nil {
			t.Fatalf("failed to login: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 OK for login, got %d", resp.StatusCode)
		}
	}

	// re-fetch csrf after login (new session)
	req, _ = http.NewRequest(http.MethodGet, url+"/api/auth/csrf", nil)
	resp, _ = client.Do(req)
	defer resp.Body.Close()
	json.NewDecoder(resp.Body).Decode(&csrfResp)
	return csrfResp.Data.CSRFToken
}

func registerUser(t *testing.T, client *http.Client, url string, name, email, password string) string {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	client.Jar = jar

	registerBody := strings.NewReader(`{"name":"` + name + `","email":"` + email + `","password":"` + password + `"}`)
	req, _ := http.NewRequest(http.MethodPost, url+"/api/auth/register", registerBody)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("failed to register: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 200/201 OK for register, got %d", resp.StatusCode)
	}

	// Now get CSRF token using the session established by register
	req, _ = http.NewRequest(http.MethodGet, url+"/api/auth/csrf", nil)
	resp, _ = client.Do(req)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var csrfResp struct {
		Data struct {
			CSRFToken string `json:"csrf_token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &csrfResp); err != nil {
		t.Fatalf("failed to unmarshal csrf response: %v, body: %s", err, string(body))
	}
	if csrfResp.Data.CSRFToken == "" {
		t.Fatalf("empty csrf token. body: %s", string(body))
	}
	return csrfResp.Data.CSRFToken
}

func TestBusinessCreation(t *testing.T) {
	pool, handler := setupApp(t)
	server := httptest.NewServer(handler)
	defer server.Close()

	client := server.Client()
	csrfToken := registerUser(t, client, server.URL, "Owner", "owner@test.com", "password123")

	// Create business
	body := strings.NewReader(`{"name":"My Business", "business_type":"RETAIL", "currency":"IDR", "timezone":"Asia/Jakarta"}`)
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/businesses", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrfToken)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("failed to create business: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 200/201 OK for create business, got %d", resp.StatusCode)
	}

	var bizResp struct {
		Data struct {
			Code           string   `json:"code"`
			Role           string   `json:"role"`
			EnabledModules []string `json:"enabled_modules"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&bizResp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if bizResp.Data.Code == "" {
		t.Fatalf("expected business code to be returned")
	}
	if bizResp.Data.Role != "OWNER" {
		t.Fatalf("expected role OWNER, got %s", bizResp.Data.Role)
	}
	wantModules := []string{"CATALOG", "INVENTORY", "SALES", "PURCHASE", "FINANCE", "REPORTING"}
	if !reflect.DeepEqual(bizResp.Data.EnabledModules, wantModules) {
		t.Fatalf("enabled_modules = %#v, want %#v", bizResp.Data.EnabledModules, wantModules)
	}
	var defaultCashCount, sequenceCount int
	if err := pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM cash_accounts ca
		JOIN businesses b ON b.id = ca.business_id
		WHERE b.public_code = $1 AND ca.is_default`, bizResp.Data.Code).Scan(&defaultCashCount); err != nil {
		t.Fatalf("query default cash account: %v", err)
	}
	if err := pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM number_sequences ns
		JOIN businesses b ON b.id = ns.business_id
		WHERE b.public_code = $1 AND ns.sequence_type IN ('PRODUCT', 'OPENING_STOCK', 'STOCK_ADJUSTMENT', 'CONTACT', 'CASH', 'SALE', 'PURC', 'PAY')`, bizResp.Data.Code).Scan(&sequenceCount); err != nil {
		t.Fatalf("query business sequences: %v", err)
	}
	if defaultCashCount != 1 || sequenceCount != 8 {
		t.Fatalf("onboarding defaults: cash=%d, sequences=%d", defaultCashCount, sequenceCount)
	}
}

func createBusiness(t *testing.T, client *http.Client, url, csrfToken, name string) string {
	body := strings.NewReader(`{"name":"` + name + `", "business_type":"RETAIL", "currency":"IDR", "timezone":"Asia/Jakarta"}`)
	req, _ := http.NewRequest(http.MethodPost, url+"/api/businesses", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrfToken)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("failed to create business: %v", err)
	}
	defer resp.Body.Close()
	var bizResp struct {
		Data struct {
			Code string `json:"code"`
		} `json:"data"`
	}
	json.NewDecoder(resp.Body).Decode(&bizResp)
	return bizResp.Data.Code
}

func switchBusiness(t *testing.T, client *http.Client, url, csrfToken, code string) {
	body := strings.NewReader(`{"business_code":"` + code + `"}`)
	req, _ := http.NewRequest(http.MethodPost, url+"/api/auth/switch-business", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrfToken)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("failed to switch business: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 OK for switch business, got %d. body: %s", resp.StatusCode, string(bodyBytes))
	}
}

func TestCrossTenantIsolation(t *testing.T) {
	_, handler := setupApp(t)
	server := httptest.NewServer(handler)
	defer server.Close()

	clientA := &http.Client{}
	csrfA := registerUser(t, clientA, server.URL, "User A", "a@test.com", "password")
	bizA := createBusiness(t, clientA, server.URL, csrfA, "Business A")

	clientB := &http.Client{}
	csrfB := registerUser(t, clientB, server.URL, "User B", "b@test.com", "password")
	bizB := createBusiness(t, clientB, server.URL, csrfB, "Business B")

	// User B tries to switch to Business A
	switchBody := strings.NewReader(`{"business_code":"` + bizA + `"}`)
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/auth/switch-business", switchBody)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrfB)
	resp, _ := clientB.Do(req)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusNotFound {
		bodyBytes, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 403 or 404 for cross-tenant switch, got %d. body: %s", resp.StatusCode, string(bodyBytes))
	}
	bodyBytes, _ := io.ReadAll(resp.Body)
	t.Logf("Cross tenant switch body: %s", string(bodyBytes))

	// Create product in Business B
	switchBusiness(t, clientB, server.URL, csrfB, bizB)
	body := strings.NewReader(`{"name":"Product B", "sku":"","barcode":"","base_unit_symbol":"","default_purchase_price":"0","min_stock":"0","default_selling_price":"1000", "is_stock_tracked":true}`)
	req, _ = http.NewRequest(http.MethodPost, server.URL+"/api/products", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrfB)
	resp, _ = clientB.Do(req)
	defer resp.Body.Close()
	var prodResp struct {
		Data struct {
			Code string `json:"code"`
		} `json:"data"`
	}
	json.NewDecoder(resp.Body).Decode(&prodResp)

	// User A tries to delete product in Business B
	switchBusiness(t, clientA, server.URL, csrfA, bizA)
	req, _ = http.NewRequest(http.MethodDelete, server.URL+"/api/products/"+prodResp.Data.Code, nil)
	req.Header.Set("X-CSRF-Token", csrfA)
	resp, _ = clientA.Do(req)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-tenant delete, got %d", resp.StatusCode)
	}

	// User A creates a product in Business A
	switchBusiness(t, clientA, server.URL, csrfA, bizA)
	bodyA := strings.NewReader(`{"name":"Product A", "sku":"","barcode":"","base_unit_symbol":"","default_purchase_price":"0","min_stock":"0","default_selling_price":"1000", "is_stock_tracked":true}`)
	req, _ = http.NewRequest(http.MethodPost, server.URL+"/api/products", bodyA)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrfA)
	resp, _ = clientA.Do(req)
	defer resp.Body.Close()
	var prodRespA struct {
		Data struct {
			Code string `json:"code"`
		} `json:"data"`
	}
	json.NewDecoder(resp.Body).Decode(&prodRespA)

	// User A creates a purchase in Business A
	purBody := `{"location_code":"LOC-DEFAULT","supplier_code":"","payment_status":"UNPAID","discount_total":"0","tax_total":"0","items":[{"product_code":"` + prodRespA.Data.Code + `","quantity":"1","unit_price":"100","discount":"0"}]}`
	req, _ = http.NewRequest(http.MethodPost, server.URL+"/api/purchases", strings.NewReader(purBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrfA)
	resp, _ = clientA.Do(req)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(resp.Body)
		t.Fatalf("failed to create purchase A, got %d. body: %s", resp.StatusCode, string(bodyBytes))
	}
	var purchaseRespA struct {
		Data struct {
			Number string `json:"purchase_number"`
		} `json:"data"`
	}
	json.NewDecoder(resp.Body).Decode(&purchaseRespA)

	// User B tries to receive User A's purchase
	switchBusiness(t, clientB, server.URL, csrfB, bizB)
	req, _ = http.NewRequest(http.MethodPost, server.URL+"/api/purchases/"+purchaseRespA.Data.Number+"/receive", nil)
	req.Header.Set("X-CSRF-Token", csrfB)
	resp, _ = clientB.Do(req)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-tenant purchase receive, got %d", resp.StatusCode)
	}
}

func TestProductTrackingLifecycle(t *testing.T) {
	_, handler := setupApp(t)
	server := httptest.NewServer(handler)
	defer server.Close()

	client := server.Client()
	csrf := registerUser(t, client, server.URL, "User C", "c@test.com", "password")
	biz := createBusiness(t, client, server.URL, csrf, "Business C")
	switchBusiness(t, client, server.URL, csrf, biz)

	// 1. Create product
	body := strings.NewReader(`{"name":"Product C", "sku":"", "barcode":"", "base_unit_symbol":"", "default_purchase_price":"0", "default_selling_price":"1000", "min_stock":"0", "is_stock_tracked":true}`)
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/products", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrf)
	resp, _ := client.Do(req)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(resp.Body)
		t.Fatalf("failed to create product, got %d. body: %s", resp.StatusCode, string(bodyBytes))
	}

	var prodResp struct {
		Data struct {
			Code string `json:"code"`
		} `json:"data"`
	}
	json.NewDecoder(resp.Body).Decode(&prodResp)
	code := prodResp.Data.Code

	// 2. Opening stock
	osBody := strings.NewReader(`{"product_code":"` + code + `", "location_code":"LOC-DEFAULT", "quantity":"10", "reason":""}`)
	req, _ = http.NewRequest(http.MethodPost, server.URL+"/api/inventory/opening-stock", osBody)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrf)
	resp, _ = client.Do(req)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(resp.Body)
		t.Fatalf("failed to create opening stock, got %d. body: %s", resp.StatusCode, string(bodyBytes))
	}

	// 3. Verify inventory
	req, _ = http.NewRequest(http.MethodGet, server.URL+"/api/inventory/products", nil)
	resp, _ = client.Do(req)
	defer resp.Body.Close()
	var invResp struct {
		Data struct {
			Items []struct {
				Code     string `json:"product_code"`
				Quantity string `json:"quantity"`
			} `json:"items"`
		} `json:"data"`
	}
	json.NewDecoder(resp.Body).Decode(&invResp)
	if len(invResp.Data.Items) == 0 || (invResp.Data.Items[0].Quantity != "10.0000" && invResp.Data.Items[0].Quantity != "10") {
		t.Fatalf("inventory should have quantity 10, got %v", invResp.Data.Items)
	}
}

func TestDuplicateOpeningStock(t *testing.T) {
	_, handler := setupApp(t)
	server := httptest.NewServer(handler)
	defer server.Close()

	client := server.Client()
	csrf := registerUser(t, client, server.URL, "User D", "d@test.com", "password")
	biz := createBusiness(t, client, server.URL, csrf, "Business D")
	switchBusiness(t, client, server.URL, csrf, biz)

	body := strings.NewReader(`{"name":"Product D", "sku":"","barcode":"","base_unit_symbol":"","default_purchase_price":"0","min_stock":"0","default_selling_price":"1000", "is_stock_tracked":true}`)
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/products", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrf)
	resp, _ := client.Do(req)
	defer resp.Body.Close()
	var prodResp struct {
		Data struct {
			Code string `json:"code"`
		} `json:"data"`
	}
	json.NewDecoder(resp.Body).Decode(&prodResp)

	// Opening stock 1
	osBody := `{"product_code":"` + prodResp.Data.Code + `", "location_code":"LOC-DEFAULT", "quantity":"10", "reason":""}`
	req, _ = http.NewRequest(http.MethodPost, server.URL+"/api/inventory/opening-stock", strings.NewReader(osBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrf)
	resp, _ = client.Do(req)
	resp.Body.Close()

	// Opening stock 2
	req, _ = http.NewRequest(http.MethodPost, server.URL+"/api/inventory/opening-stock", strings.NewReader(osBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrf)
	resp, _ = client.Do(req)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict && resp.StatusCode != http.StatusBadRequest && resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected conflict error for duplicate opening stock, got %d", resp.StatusCode)
	}
}

func TestTransactionRollback(t *testing.T) {
	_, handler := setupApp(t)
	server := httptest.NewServer(handler)
	defer server.Close()

	client := server.Client()
	csrf := registerUser(t, client, server.URL, "User E", "e@test.com", "password")
	biz := createBusiness(t, client, server.URL, csrf, "Business E")
	switchBusiness(t, client, server.URL, csrf, biz)

	body := strings.NewReader(`{"name":"Product E", "sku":"","barcode":"","base_unit_symbol":"","default_purchase_price":"0","min_stock":"0","default_selling_price":"1000", "is_stock_tracked":true}`)
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/products", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrf)
	resp, _ := client.Do(req)
	defer resp.Body.Close()
	var prodResp struct {
		Data struct {
			Code string `json:"code"`
		} `json:"data"`
	}
	json.NewDecoder(resp.Body).Decode(&prodResp)

	// Send invalid stock adjustment to cause rollback
	adjBody := `{"product_code":"` + prodResp.Data.Code + `", "location_code":"LOC-DEFAULT", "difference":-100, "reason":"TEST"}`
	req, _ = http.NewRequest(http.MethodPost, server.URL+"/api/inventory/adjustments", strings.NewReader(adjBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrf)
	resp, _ = client.Do(req)
	defer resp.Body.Close()

	// The adjustment should fail or fail at complete. If it succeeds, complete it to fail.
	var adjResp struct {
		Data struct {
			Number string `json:"number"`
		} `json:"data"`
	}
	if resp.StatusCode == http.StatusOK {
		json.NewDecoder(resp.Body).Decode(&adjResp)
		req, _ = http.NewRequest(http.MethodPost, server.URL+"/api/inventory/adjustments/"+adjResp.Data.Number+"/complete", nil)
		req.Header.Set("X-CSRF-Token", csrf)
		resp, _ = client.Do(req)
		resp.Body.Close()
	}

	// Verify stock is still 0
	req, _ = http.NewRequest(http.MethodGet, server.URL+"/api/inventory/products", nil)
	resp, _ = client.Do(req)
	defer resp.Body.Close()
	var invResp struct {
		Data struct {
			Items []struct {
				Quantity string `json:"quantity"`
			} `json:"items"`
		} `json:"data"`
	}
	json.NewDecoder(resp.Body).Decode(&invResp)
	if len(invResp.Data.Items) > 0 && invResp.Data.Items[0].Quantity != "0" && invResp.Data.Items[0].Quantity != "" {
		t.Fatalf("expected stock 0, got %v", invResp.Data.Items[0].Quantity)
	}
}

func TestPurchaseWorkflow(t *testing.T) {
	_, handler := setupApp(t)
	server := httptest.NewServer(handler)
	defer server.Close()

	client := server.Client()
	csrf := registerUser(t, client, server.URL, "User F", "f@test.com", "password")
	biz := createBusiness(t, client, server.URL, csrf, "Business F")
	switchBusiness(t, client, server.URL, csrf, biz)

	// 2. Create product
	prodBody := strings.NewReader(`{"name":"Product P", "sku":"", "barcode":"", "base_unit_symbol":"", "default_purchase_price":"0", "default_selling_price":"2000", "min_stock":"0", "is_stock_tracked":true}`)
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/products", prodBody)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrf)
	resp, _ := client.Do(req)
	defer resp.Body.Close()
	var prodResp struct {
		Data struct {
			Code string `json:"code"`
		} `json:"data"`
	}
	json.NewDecoder(resp.Body).Decode(&prodResp)

	// Create Cash Account
	cashBody := `{"Name":"Kas Utama","AccountType":"CASH","Balance":"10000","IsDefault":true}`
	req, _ = http.NewRequest(http.MethodPost, server.URL+"/api/cash-accounts", strings.NewReader(cashBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrf)
	resp, _ = client.Do(req)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		t.Fatalf("failed to create cash account, got %d. body: %s", resp.StatusCode, string(bodyBytes))
	}
	var cashResp struct {
		Data struct {
			Code string `json:"code"`
		} `json:"data"`
	}
	json.NewDecoder(resp.Body).Decode(&cashResp)

	// Create Draft Purchase
	purBody := `{"location_code":"LOC-DEFAULT","supplier_code":"","payment_status":"UNPAID","discount_total":"0","tax_total":"0","items":[{"product_code":"` + prodResp.Data.Code + `","quantity":"10","unit_price":"500","discount":"0"}]}`
	req, _ = http.NewRequest(http.MethodPost, server.URL+"/api/purchases", strings.NewReader(purBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrf)
	resp, _ = client.Do(req)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 201 for create purchase, got %d. body: %s", resp.StatusCode, string(bodyBytes))
	}

	var purchaseResp struct {
		Data struct {
			Number string `json:"purchase_number"`
		} `json:"data"`
	}
	json.NewDecoder(resp.Body).Decode(&purchaseResp)

	// Receive Purchase
	req, _ = http.NewRequest(http.MethodPost, server.URL+"/api/purchases/"+purchaseResp.Data.Number+"/receive", nil)
	req.Header.Set("X-CSRF-Token", csrf)
	resp, _ = client.Do(req)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for receive purchase, got %d", resp.StatusCode)
	}

	// Try to receive it again (Receive dua kali)
	req, _ = http.NewRequest(http.MethodPost, server.URL+"/api/purchases/"+purchaseResp.Data.Number+"/receive", nil)
	req.Header.Set("X-CSRF-Token", csrf)
	resp, _ = client.Do(req)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for duplicate receive, got %d", resp.StatusCode)
	}

	// Verify Inventory increased to 10
	req, _ = http.NewRequest(http.MethodGet, server.URL+"/api/inventory/products", nil)
	resp, _ = client.Do(req)
	defer resp.Body.Close()
	var invResp struct {
		Data struct {
			Items []struct {
				Quantity string `json:"quantity"`
			} `json:"items"`
		} `json:"data"`
	}
	json.NewDecoder(resp.Body).Decode(&invResp)
	if len(invResp.Data.Items) == 0 || (invResp.Data.Items[0].Quantity != "10.0000" && invResp.Data.Items[0].Quantity != "10") {
		t.Fatalf("expected stock 10, got %v", invResp.Data.Items[0].Quantity)
	}

	// Pay Purchase
	payBody := `{"cash_account_code":"` + cashResp.Data.Code + `","amount":"5000"}`
	req, _ = http.NewRequest(http.MethodPost, server.URL+"/api/purchases/"+purchaseResp.Data.Number+"/payments", strings.NewReader(payBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrf)
	resp, _ = client.Do(req)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 for pay purchase, got %d", resp.StatusCode)
	}
}
