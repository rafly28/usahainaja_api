//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"usahainaja/backend/db/migrations"
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

func TestAuthEndpoints(t *testing.T) {
	_, handler := setupApp(t)
	server := httptest.NewServer(handler)
	defer server.Close()

	client := server.Client()

	// Wait, we need a cookie jar for sessions.
	jar, _ := newCookieJar()
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
	// STOCK_OPNAME ditambahkan pada T12 untuk nomor opname terpisah dari adjustment biasa.
	if err := pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM number_sequences ns
		JOIN businesses b ON b.id = ns.business_id
		WHERE b.public_code = $1 AND ns.sequence_type IN ('PRODUCT', 'OPENING_STOCK', 'STOCK_ADJUSTMENT', 'STOCK_OPNAME', 'CONTACT', 'CASH', 'SALE', 'PURC', 'PAY')`, bizResp.Data.Code).Scan(&sequenceCount); err != nil {
		t.Fatalf("query business sequences: %v", err)
	}
	if defaultCashCount != 1 || sequenceCount != 9 {
		t.Fatalf("onboarding defaults: cash=%d, sequences=%d", defaultCashCount, sequenceCount)
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
	resp := mustDo(t, clientB, req)
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
	resp = mustDo(t, clientB, req)
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
	resp = mustDo(t, clientA, req)
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
	resp = mustDo(t, clientA, req)
	defer resp.Body.Close()
	var prodRespA struct {
		Data struct {
			Code string `json:"code"`
		} `json:"data"`
	}
	json.NewDecoder(resp.Body).Decode(&prodRespA)

	// User A creates a purchase in Business A
	purBody := `{"location_code":"LOC-DEFAULT","supplier_code":"","discount_total":"0","tax_total":"0","items":[{"product_code":"` + prodRespA.Data.Code + `","quantity":"1","unit_price":"100","discount":"0"}]}`
	purchaseNumberA := postIdempotent(t, clientA, server.URL, csrfA, "/api/purchases", purBody, "purchase_number", http.StatusCreated)

	// User B tries to receive User A's purchase
	switchBusiness(t, clientB, server.URL, csrfB, bizB)
	status, _ := postIdempotentStatus(t, clientB, server.URL, csrfB,
		"/api/purchases/"+purchaseNumberA+"/receive",
		`{"items":[{"line_number":1,"received_quantity":"1"}]}`)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-tenant purchase receive, got %d", status)
	}

	// User B tries to read User A's stock adjustment list and movements
	req, _ = http.NewRequest(http.MethodGet, server.URL+"/api/inventory/adjustments", nil)
	resp = mustDo(t, clientB, req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for own adjustment list, got %d", resp.StatusCode)
	}
}
