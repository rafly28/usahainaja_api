//go:build integration

package integration

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"usahainaja/backend/internal/app"
	"usahainaja/backend/internal/httpapi"
	"usahainaja/backend/internal/postgres"
)

// mustDo mengirim request dan gagal cepat bila koneksi bermasalah.
func mustDo(t *testing.T, client *http.Client, req *http.Request) *http.Response {
	t.Helper()
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s failed: %v", req.Method, req.URL.Path, err)
	}
	return resp
}

// newCookieJar membuat cookie jar baru untuk klien HTTP pengujian.
func newCookieJar() (*cookiejar.Jar, error) {
	return cookiejar.New(nil)
}

func setupApp(t *testing.T) (*pgxpool.Pool, http.Handler) {
	pool, _, cleanup := setupTestDB(t)
	t.Cleanup(cleanup)

	repo := postgres.New(pool)
	service := app.NewService(repo, time.Hour, 10) // low bcrypt cost for tests
	handler := httpapi.New(service, "test_session", false)
	return pool, handler
}

// newTestServer menyiapkan server, client, dan sesi bisnis aktif untuk pengujian.
func newTestServer(t *testing.T, userName, email, businessName string) (*httptest.Server, *http.Client, string) {
	t.Helper()
	_, handler := setupApp(t)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client := server.Client()
	csrf := registerUser(t, client, server.URL, userName, email, "password")
	business := createBusiness(t, client, server.URL, csrf, businessName)
	switchBusiness(t, client, server.URL, csrf, business)
	return server, client, csrf
}

func loginAndGetCSRF(t *testing.T, client *http.Client, url, email, password string) string {
	t.Helper()

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
	resp = mustDo(t, client, req)
	defer resp.Body.Close()
	json.NewDecoder(resp.Body).Decode(&csrfResp)
	return csrfResp.Data.CSRFToken
}

func registerUser(t *testing.T, client *http.Client, url, name, email, password string) string {
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
	resp = mustDo(t, client, req)
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

// createProduct membuat produk tracked dan mengembalikan public code-nya.
func createProduct(t *testing.T, client *http.Client, url, csrfToken, name string, extra string) string {
	t.Helper()
	if extra != "" {
		extra = "," + extra
	}
	body := `{"name":"` + name + `","sku":"","barcode":"","base_unit_symbol":"","default_purchase_price":"0","default_selling_price":"1000","min_stock":"0","is_stock_tracked":true` + extra + `}`
	return postAndDecodeField(t, client, url, csrfToken, http.MethodPost, "/api/products", body, "code", http.StatusCreated, http.StatusOK)
}

// idempotencyKey membuat UUID v4 sederhana untuk header Idempotency-Key.
func idempotencyKey(t *testing.T) string {
	t.Helper()
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		t.Fatalf("failed to build idempotency key: %v", err)
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:16])
}

// postIdempotent mengirim POST dengan header Idempotency-Key dan membaca satu field.
func postIdempotent(t *testing.T, client *http.Client, url, csrfToken, path, body, field string, allowedStatus ...int) string {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrfToken)
	req.Header.Set("Idempotency-Key", idempotencyKey(t))
	resp := mustDo(t, client, req)
	defer resp.Body.Close()
	payload, _ := io.ReadAll(resp.Body)
	if !statusAllowed(resp.StatusCode, allowedStatus) {
		t.Fatalf("POST %s: unexpected status %d. body: %s", path, resp.StatusCode, string(payload))
	}
	return decodeDataField(t, payload, field)
}

// postIdempotentStatus mengirim POST dengan header Idempotency-Key dan mengembalikan status.
func postIdempotentStatus(t *testing.T, client *http.Client, url, csrfToken, path, body string) (int, string) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, _ := http.NewRequest(http.MethodPost, url+path, reader)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrfToken)
	req.Header.Set("Idempotency-Key", idempotencyKey(t))
	resp := mustDo(t, client, req)
	defer resp.Body.Close()
	payload, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(payload)
}

// postAndDecodeField mengirim JSON dan membaca satu field dari objek data.
func postAndDecodeField(t *testing.T, client *http.Client, url, csrfToken, method, path, body, field string, allowedStatus ...int) string {
	t.Helper()
	req, _ := http.NewRequest(method, url+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if csrfToken != "" {
		req.Header.Set("X-CSRF-Token", csrfToken)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s failed: %v", method, path, err)
	}
	defer resp.Body.Close()

	payload, _ := io.ReadAll(resp.Body)
	if !statusAllowed(resp.StatusCode, allowedStatus) {
		t.Fatalf("%s %s: unexpected status %d. body: %s", method, path, resp.StatusCode, string(payload))
	}
	return decodeDataField(t, payload, field)
}

// postAndGetStatus mengirim JSON dan hanya mengembalikan status code.
func postAndGetStatus(t *testing.T, client *http.Client, url, csrfToken, method, path, body string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(method, url+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if csrfToken != "" {
		req.Header.Set("X-CSRF-Token", csrfToken)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s failed: %v", method, path, err)
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(payload)
}

// getAndDecode mengirim GET lalu mendekode payload. Status di luar 2xx menggagalkan test.
func getAndDecode(t *testing.T, client *http.Client, url, path string, destination any) int {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url+path, nil)
	resp := mustDo(t, client, req)
	defer resp.Body.Close()
	payload, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		t.Fatalf("GET %s: unexpected status %d. body: %s", path, resp.StatusCode, string(payload))
	}
	if err := json.Unmarshal(payload, destination); err != nil {
		t.Fatalf("failed to decode GET %s: %v. body: %s", path, err, string(payload))
	}
	return resp.StatusCode
}

func statusAllowed(status int, allowed []int) bool {
	for _, candidate := range allowed {
		if status == candidate {
			return true
		}
	}
	return false
}

// unitCodeBySymbol mencari kode publik satuan berdasarkan simbolnya.
func unitCodeBySymbol(t *testing.T, client *http.Client, url, symbol string) string {
	t.Helper()
	var response struct {
		Data struct {
			Items []struct {
				Code   string `json:"code"`
				Symbol string `json:"symbol"`
			} `json:"items"`
		} `json:"data"`
	}
	getAndDecode(t, client, url, "/api/units", &response)
	for _, item := range response.Data.Items {
		if strings.EqualFold(item.Symbol, symbol) {
			return item.Code
		}
	}
	t.Fatalf("unit %q not found", symbol)
	return ""
}

// firstCashAccountCode membaca kode akun kas pertama milik bisnis aktif.
func firstCashAccountCode(t *testing.T, client *http.Client, url string) string {
	t.Helper()
	var response struct {
		Data []struct {
			Code string `json:"code"`
		} `json:"data"`
	}
	getAndDecode(t, client, url, "/api/cash-accounts", &response)
	if len(response.Data) == 0 {
		t.Fatal("expected at least one cash account from onboarding")
	}
	return response.Data[0].Code
}

func decodeDataField(t *testing.T, payload []byte, field string) string {
	t.Helper()
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		t.Fatalf("failed to decode response: %v. body: %s", err, string(payload))
	}
	value, _ := envelope.Data[field].(string)
	return value
}

// inventoryQuantity membaca kuantitas stok satu produk pada lokasi default.
func inventoryQuantity(t *testing.T, client *http.Client, url, productCode string) string {
	t.Helper()
	var response struct {
		Data struct {
			Items []struct {
				ProductCode string `json:"product_code"`
				Location    string `json:"location_code"`
				Quantity    string `json:"quantity"`
			} `json:"items"`
		} `json:"data"`
	}
	status := getAndDecode(t, client, url, "/api/inventory/products?location=LOC-DEFAULT", &response)
	if status != http.StatusOK {
		t.Fatalf("GET inventory products: unexpected status %d", status)
	}
	for _, item := range response.Data.Items {
		if item.ProductCode == productCode {
			return item.Quantity
		}
	}
	return ""
}
