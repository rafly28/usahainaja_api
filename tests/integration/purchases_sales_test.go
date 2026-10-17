//go:build integration

package integration

import (
	"net/http"
	"testing"
)

func TestPurchaseWorkflow(t *testing.T) {
	server, client, csrf := newTestServer(t, "User F", "f@test.com", "Business F")

	prodCode := createProduct(t, client, server.URL, csrf, "Product P", `"default_selling_price":"2000"`)

	// Cash account default sudah dibuat saat onboarding.
	cashCode := firstCashAccountCode(t, client, server.URL)

	// Create Draft Purchase
	purchaseNumber := postIdempotent(t, client, server.URL, csrf, "/api/purchases",
		`{"location_code":"LOC-DEFAULT","supplier_code":"","discount_total":"0","tax_total":"0","items":[{"product_code":"`+prodCode+`","quantity":"10","unit_price":"500","discount":"0"}]}`,
		"purchase_number", http.StatusCreated, http.StatusOK)

	// Receive Purchase
	status, body := postIdempotentStatus(t, client, server.URL, csrf,
		"/api/purchases/"+purchaseNumber+"/receive",
		`{"items":[{"line_number":1,"received_quantity":"10"}]}`)
	// Receive membuat dokumen baru sehingga wajar dijawab 201 Created.
	if !statusAllowed(status, []int{http.StatusOK, http.StatusCreated}) {
		t.Fatalf("expected 200/201 for receive purchase, got %d. body: %s", status, body)
	}

	// Try to receive it again (Receive dua kali)
	status, _ = postIdempotentStatus(t, client, server.URL, csrf,
		"/api/purchases/"+purchaseNumber+"/receive",
		`{"items":[{"line_number":1,"received_quantity":"10"}]}`)
	// Receive kedua kalinya ditolak karena purchase sudah COMPLETED.
	if status != http.StatusConflict && status != http.StatusNotFound {
		t.Fatalf("expected 404/409 for duplicate receive, got %d", status)
	}

	// Verify Inventory increased to 10
	if quantity := inventoryQuantity(t, client, server.URL, prodCode); quantity != "10.0000" && quantity != "10" {
		t.Fatalf("expected stock 10, got %q", quantity)
	}

	// Pay Purchase
	status, body = postIdempotentStatus(t, client, server.URL, csrf,
		"/api/purchases/"+purchaseNumber+"/payments",
		`{"cash_account_code":"`+cashCode+`","amount":"5000"}`)
	if status != http.StatusCreated {
		t.Fatalf("expected 201 for pay purchase, got %d. body: %s", status, body)
	}
}

func TestSalesCheckoutConcurrency(t *testing.T) {
	server, client, csrf := newTestServer(t, "User G", "g@test.com", "Business G")

	prodCode := createProduct(t, client, server.URL, csrf, "Product G", "")

	cashCode := firstCashAccountCode(t, client, server.URL)

	// Opening Stock (1 unit)
	status, body := postAndGetStatus(t, client, server.URL, csrf, http.MethodPost, "/api/inventory/opening-stock",
		`{"location_code":"LOC-DEFAULT","items":[{"product_code":"`+prodCode+`","quantity":"1"}],"reason":""}`)
	if !statusAllowed(status, []int{http.StatusOK, http.StatusCreated}) {
		t.Fatalf("Failed to create opening stock: %d %s", status, body)
	}

	saleBody := `{"location_code":"LOC-DEFAULT", "customer_code": "",  "discount_total":"0", "tax_total":"0", "items":[{"product_code":"` + prodCode + `", "quantity":"1", "unit_price":"1000", "discount":"0"}]}`
	firstSale := postIdempotent(t, client, server.URL, csrf, "/api/sales", saleBody, "receipt_number", http.StatusCreated)
	secondSale := postIdempotent(t, client, server.URL, csrf, "/api/sales", saleBody, "receipt_number", http.StatusCreated)

	// Concurrent Checkout
	type checkoutResult struct {
		code int
		body string
	}
	results := make(chan checkoutResult, 2)
	checkout := func(receipt string) {
		payBody := `{"cash_account_code":"` + cashCode + `", "amount":"1000"}`
		status, responseBody := postIdempotentStatus(t, client, server.URL, csrf,
			"/api/sales/"+receipt+"/checkout", payBody)
		results <- checkoutResult{status, responseBody}
	}

	go checkout(firstSale)
	go checkout(secondSale)

	first := <-results
	second := <-results

	bothSucceeded := first.code == http.StatusOK && second.code == http.StatusOK
	bothFailed := first.code != http.StatusOK && second.code != http.StatusOK
	if bothSucceeded || bothFailed {
		t.Fatalf("expected one checkout to succeed and one to fail due to stock, got %d (%s) and %d (%s)",
			first.code, first.body, second.code, second.body)
	}
}
