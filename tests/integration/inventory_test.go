//go:build integration

package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProductTrackingLifecycle(t *testing.T) {
	server, client, csrf := newTestServer(t, "User C", "c@test.com", "Business C")

	code := createProduct(t, client, server.URL, csrf, "Product C", "")

	// Opening stock memakai batch items[] sesuai kontrak.
	status, body := postAndGetStatus(t, client, server.URL, csrf, http.MethodPost, "/api/inventory/opening-stock",
		`{"location_code":"LOC-DEFAULT","items":[{"product_code":"`+code+`","quantity":"10","unit_symbol":""}],"reason":""}`)
	if !statusAllowed(status, []int{http.StatusOK, http.StatusCreated}) {
		t.Fatalf("failed to create opening stock, got %d. body: %s", status, body)
	}

	if quantity := inventoryQuantity(t, client, server.URL, code); quantity != "10.0000" && quantity != "10" {
		t.Fatalf("inventory should have quantity 10, got %q", quantity)
	}
}

func TestOpeningStockAcceptsBatchItems(t *testing.T) {
	server, client, csrf := newTestServer(t, "User Batch", "batch@test.com", "Business Batch")

	first := createProduct(t, client, server.URL, csrf, "Batch One", "")
	second := createProduct(t, client, server.URL, csrf, "Batch Two", "")

	status, body := postAndGetStatus(t, client, server.URL, csrf, http.MethodPost, "/api/inventory/opening-stock",
		`{"location_code":"LOC-DEFAULT","items":[`+
			`{"product_code":"`+first+`","quantity":"5"},`+
			`{"product_code":"`+second+`","quantity":"7"}],"reason":"stok awal jointly"}`)
	if !statusAllowed(status, []int{http.StatusOK, http.StatusCreated}) {
		t.Fatalf("batch opening stock failed, got %d. body: %s", status, body)
	}

	if quantity := inventoryQuantity(t, client, server.URL, first); quantity != "5.0000" && quantity != "5" {
		t.Fatalf("first product should have 5, got %q", quantity)
	}
	if quantity := inventoryQuantity(t, client, server.URL, second); quantity != "7.0000" && quantity != "7" {
		t.Fatalf("second product should have 7, got %q", quantity)
	}
}

func TestDuplicateOpeningStock(t *testing.T) {
	server, client, csrf := newTestServer(t, "User D", "d@test.com", "Business D")

	code := createProduct(t, client, server.URL, csrf, "Product D", "")
	osBody := `{"location_code":"LOC-DEFAULT","items":[{"product_code":"` + code + `","quantity":"10"}],"reason":""}`

	if status, body := postAndGetStatus(t, client, server.URL, csrf, http.MethodPost, "/api/inventory/opening-stock", osBody); !statusAllowed(status, []int{http.StatusOK, http.StatusCreated}) {
		t.Fatalf("first opening stock failed, got %d. body: %s", status, body)
	}

	status, _ := postAndGetStatus(t, client, server.URL, csrf, http.MethodPost, "/api/inventory/opening-stock", osBody)
	if status != http.StatusConflict && status != http.StatusBadRequest && status != http.StatusUnprocessableEntity {
		t.Fatalf("expected conflict error for duplicate opening stock, got %d", status)
	}
}

func TestTransactionRollback(t *testing.T) {
	server, client, csrf := newTestServer(t, "User E", "e@test.com", "Business E")

	code := createProduct(t, client, server.URL, csrf, "Product E", "")

	// Adjustment OUT melebihi stok tidak boleh menyimpan snapshot negatif.
	number := postAndDecodeField(t, client, server.URL, csrf, http.MethodPost, "/api/inventory/adjustments",
		`{"location_code":"LOC-DEFAULT","reason":"LOST","items":[{"product_code":"`+code+`","quantity":"100","direction":"OUT","unit_symbol":""}]}`,
		"adjustment_number", http.StatusCreated)

	status, body := postAndGetStatus(t, client, server.URL, csrf, http.MethodPost,
		"/api/inventory/adjustments/"+number+"/complete", "")
	if status != http.StatusConflict {
		t.Fatalf("expected 409 for insufficient stock, got %d. body: %s", status, body)
	}

	if quantity := inventoryQuantity(t, client, server.URL, code); quantity != "0.0000" && quantity != "0" && quantity != "" {
		t.Fatalf("stock must remain zero after failed adjustment, got %q", quantity)
	}

	// Adjustment harus tetap DRAFT sehingga dapat diperbaiki, bukan hilang.
	var detail struct {
		Data struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	getAndDecode(t, client, server.URL, "/api/inventory/adjustments/"+number, &detail)
	if detail.Data.Status != "DRAFT" {
		t.Fatalf("failed adjustment must stay DRAFT, got %q", detail.Data.Status)
	}
}

func TestCompleteAdjustmentForEveryReason(t *testing.T) {
	server, client, csrf := newTestServer(t, "User Reason", "reason@test.com", "Business Reason")

	code := createProduct(t, client, server.URL, csrf, "Product Reason", "")
	if status, body := postAndGetStatus(t, client, server.URL, csrf, http.MethodPost, "/api/inventory/opening-stock",
		`{"location_code":"LOC-DEFAULT","items":[{"product_code":"`+code+`","quantity":"50"}],"reason":""}`); !statusAllowed(status, []int{http.StatusOK, http.StatusCreated}) {
		t.Fatalf("opening stock failed, got %d. body: %s", status, body)
	}

	// Semua alasan harus dapat diselesaikan pada PostgreSQL nyata.
	for index, reason := range []string{"CORRECTION", "DAMAGE", "SPOILAGE", "LOST", "OTHER"} {
		number := postAndDecodeField(t, client, server.URL, csrf, http.MethodPost, "/api/inventory/adjustments",
			`{"location_code":"LOC-DEFAULT","reason":"`+reason+`","items":[{"product_code":"`+code+`","quantity":"1","direction":"OUT","unit_symbol":""}]}`,
			"adjustment_number", http.StatusCreated)

		status, body := postAndGetStatus(t, client, server.URL, csrf, http.MethodPost,
			"/api/inventory/adjustments/"+number+"/complete", "")
		if status != http.StatusOK {
			t.Fatalf("reason %s (index %d) failed to complete: %d. body: %s", reason, index, status, body)
		}
	}

	// Alasan yang tidak ada di enum movement_type (CORRECTION/LOST/OTHER) harus
	// dicatat sebagai ADJUSTMENT, sedangkan DAMAGE/SPOILAGE memakai tipe sendiri.
	var movements struct {
		Data struct {
			Movements []struct {
				MovementType string `json:"movement_type"`
				Reason       string `json:"reason"`
			} `json:"movements"`
		} `json:"data"`
	}
	getAndDecode(t, client, server.URL, "/api/inventory/movements?limit=50", &movements)

	wantTypeByReason := map[string]string{
		"CORRECTION": "ADJUSTMENT",
		"DAMAGE":     "DAMAGE",
		"SPOILAGE":   "SPOILAGE",
		"LOST":       "ADJUSTMENT",
		"OTHER":      "ADJUSTMENT",
	}
	seen := map[string]string{}
	for _, movement := range movements.Data.Movements {
		if movement.MovementType == "OPENING_BALANCE" {
			continue
		}
		want, tracked := wantTypeByReason[movement.Reason]
		if !tracked {
			t.Fatalf("unexpected movement reason %q", movement.Reason)
		}
		if movement.MovementType != want {
			t.Fatalf("reason %q should be stored as movement_type %q, got %q",
				movement.Reason, want, movement.MovementType)
		}
		seen[movement.Reason] = movement.MovementType
	}
	for reason := range wantTypeByReason {
		if _, ok := seen[reason]; !ok {
			t.Fatalf("expected movement for reason %q, got %#v", reason, seen)
		}
	}
}

func TestStockMovementFiltersAndPagination(t *testing.T) {
	server, client, csrf := newTestServer(t, "User Filter", "filter@test.com", "Business Filter")

	first := createProduct(t, client, server.URL, csrf, "Filter One", "")
	second := createProduct(t, client, server.URL, csrf, "Filter Two", "")

	if status, body := postAndGetStatus(t, client, server.URL, csrf, http.MethodPost, "/api/inventory/opening-stock",
		`{"location_code":"LOC-DEFAULT","items":[`+
			`{"product_code":"`+first+`","quantity":"9"},`+
			`{"product_code":"`+second+`","quantity":"3"}],"reason":""}`); !statusAllowed(status, []int{http.StatusOK, http.StatusCreated}) {
		t.Fatalf("opening stock failed, got %d. body: %s", status, body)
	}

	var byProduct struct {
		Data struct {
			Movements  []map[string]any `json:"movements"`
			Pagination struct {
				Total int `json:"total"`
			} `json:"pagination"`
		} `json:"data"`
	}
	getAndDecode(t, client, server.URL, "/api/inventory/movements?product_public_code="+first, &byProduct)
	if byProduct.Data.Pagination.Total != 1 {
		t.Fatalf("expected 1 movement for product filter, got %d", byProduct.Data.Pagination.Total)
	}

	var byType struct {
		Data struct {
			Pagination struct {
				Total int `json:"total"`
			} `json:"pagination"`
		} `json:"data"`
	}
	getAndDecode(t, client, server.URL, "/api/inventory/movements?movement_type=OPENING_BALANCE", &byType)
	if byType.Data.Pagination.Total != 2 {
		t.Fatalf("expected 2 opening balance movements, got %d", byType.Data.Pagination.Total)
	}

	var paged struct {
		Data struct {
			Movements  []map[string]any `json:"movements"`
			Pagination struct {
				Page  int `json:"page"`
				Limit int `json:"limit"`
				Total int `json:"total"`
			} `json:"pagination"`
		} `json:"data"`
	}
	getAndDecode(t, client, server.URL, "/api/inventory/movements?limit=1&page=2", &paged)
	if len(paged.Data.Movements) != 1 || paged.Data.Pagination.Page != 2 || paged.Data.Pagination.Total != 2 {
		t.Fatalf("unexpected pagination result: %#v", paged.Data)
	}

	// Filter tanggal yang tidak valid harus ditolak.
	status, _ := postAndGetStatus(t, client, server.URL, csrf, http.MethodGet, "/api/inventory/movements?date_from=01-01-2026", "")
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for invalid date filter, got %d", status)
	}

	// movement_type di luar enum harus ditolak.
	status, _ = postAndGetStatus(t, client, server.URL, csrf, http.MethodGet, "/api/inventory/movements?movement_type=CORRECTION", "")
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for invalid movement type, got %d", status)
	}
}

func TestLowStockFilterIsServerSide(t *testing.T) {
	server, client, csrf := newTestServer(t, "User Low", "low@test.com", "Business Low")

	low := createProduct(t, client, server.URL, csrf, "Low Stock", `"min_stock":"10"`)
	healthy := createProduct(t, client, server.URL, csrf, "Healthy Stock", `"min_stock":"1"`)

	if status, body := postAndGetStatus(t, client, server.URL, csrf, http.MethodPost, "/api/inventory/opening-stock",
		`{"location_code":"LOC-DEFAULT","items":[`+
			`{"product_code":"`+low+`","quantity":"2"},`+
			`{"product_code":"`+healthy+`","quantity":"50"}],"reason":""}`); !statusAllowed(status, []int{http.StatusOK, http.StatusCreated}) {
		t.Fatalf("opening stock failed, got %d. body: %s", status, body)
	}

	var response struct {
		Data struct {
			Items []struct {
				ProductCode string `json:"product_code"`
				IsLowStock  bool   `json:"is_low_stock"`
			} `json:"items"`
		} `json:"data"`
	}
	getAndDecode(t, client, server.URL, "/api/inventory/products?low_stock=true", &response)
	if len(response.Data.Items) != 1 || response.Data.Items[0].ProductCode != low {
		t.Fatalf("expected only low stock product, got %#v", response.Data.Items)
	}
	if !response.Data.Items[0].IsLowStock {
		t.Fatal("expected is_low_stock to be true")
	}
}

func TestStockOpnameGeneratesAdjustment(t *testing.T) {
	server, client, csrf := newTestServer(t, "User Opname", "opname@test.com", "Business Opname")

	code := createProduct(t, client, server.URL, csrf, "Product Opname", "")
	if status, body := postAndGetStatus(t, client, server.URL, csrf, http.MethodPost, "/api/inventory/opening-stock",
		`{"location_code":"LOC-DEFAULT","items":[{"product_code":"`+code+`","quantity":"10"}],"reason":""}`); !statusAllowed(status, []int{http.StatusOK, http.StatusCreated}) {
		t.Fatalf("opening stock failed, got %d. body: %s", status, body)
	}

	// Hasil hitung fisik lebih kecil dari sistem -> direction OUT.
	number := postAndDecodeField(t, client, server.URL, csrf, http.MethodPost, "/api/inventory/opnames",
		`{"location_code":"LOC-DEFAULT","notes":"hitung bulanan","items":[{"product_code":"`+code+`","counted_quantity":"7"}]}`,
		"adjustment_number", http.StatusCreated)
	if !strings.HasPrefix(number, "OPN-") {
		t.Fatalf("opname should use OPN prefix, got %q", number)
	}

	var detail struct {
		Data struct {
			Kind   string `json:"adjustment_kind"`
			Status string `json:"status"`
			Items  []struct {
				Direction       string `json:"direction"`
				SystemQuantity  string `json:"system_quantity"`
				CountedQuantity string `json:"counted_quantity"`
				Quantity        string `json:"quantity"`
			} `json:"items"`
		} `json:"data"`
	}
	getAndDecode(t, client, server.URL, "/api/inventory/opnames/"+number, &detail)
	if detail.Data.Kind != "OPNAME" || detail.Data.Status != "DRAFT" {
		t.Fatalf("unexpected opname header: %#v", detail.Data)
	}
	if len(detail.Data.Items) != 1 {
		t.Fatalf("expected 1 opname item, got %#v", detail.Data.Items)
	}
	item := detail.Data.Items[0]
	if item.Direction != "OUT" {
		t.Fatalf("expected OUT direction, got %q", item.Direction)
	}
	if item.SystemQuantity != "10.0000" || item.CountedQuantity != "7.0000" || item.Quantity != "3.0000" {
		t.Fatalf("unexpected opname trace: %#v", item)
	}

	if status, body := postAndGetStatus(t, client, server.URL, csrf, http.MethodPost, "/api/inventory/opnames/"+number+"/complete", ""); status != http.StatusOK {
		t.Fatalf("opname complete failed, got %d. body: %s", status, body)
	}

	if quantity := inventoryQuantity(t, client, server.URL, code); quantity != "7.0000" && quantity != "7" {
		t.Fatalf("opname should reduce stock to 7, got %q", quantity)
	}
}

func TestStockOpnameWithoutDifferenceIsRejected(t *testing.T) {
	server, client, csrf := newTestServer(t, "User NoDiff", "nodiff@test.com", "Business NoDiff")

	code := createProduct(t, client, server.URL, csrf, "Product NoDiff", "")
	if status, body := postAndGetStatus(t, client, server.URL, csrf, http.MethodPost, "/api/inventory/opening-stock",
		`{"location_code":"LOC-DEFAULT","items":[{"product_code":"`+code+`","quantity":"10"}],"reason":""}`); !statusAllowed(status, []int{http.StatusOK, http.StatusCreated}) {
		t.Fatalf("opening stock failed, got %d. body: %s", status, body)
	}

	status, body := postAndGetStatus(t, client, server.URL, csrf, http.MethodPost, "/api/inventory/opnames",
		`{"location_code":"LOC-DEFAULT","items":[{"product_code":"`+code+`","counted_quantity":"10"}]}`)
	if status != http.StatusConflict {
		t.Fatalf("expected 409 when opname has no difference, got %d. body: %s", status, body)
	}
	if !strings.Contains(body, "NO_STOCK_DIFFERENCE") {
		t.Fatalf("expected NO_STOCK_DIFFERENCE code, got body: %s", body)
	}
}

func TestStockAdjustmentListAndDraftLifecycle(t *testing.T) {
	server, client, csrf := newTestServer(t, "User Draft", "draft@test.com", "Business Draft")

	code := createProduct(t, client, server.URL, csrf, "Product Draft", "")
	if status, body := postAndGetStatus(t, client, server.URL, csrf, http.MethodPost, "/api/inventory/opening-stock",
		`{"location_code":"LOC-DEFAULT","items":[{"product_code":"`+code+`","quantity":"10"}],"reason":""}`); !statusAllowed(status, []int{http.StatusOK, http.StatusCreated}) {
		t.Fatalf("opening stock failed, got %d. body: %s", status, body)
	}

	draft := postAndDecodeField(t, client, server.URL, csrf, http.MethodPost, "/api/inventory/adjustments",
		`{"location_code":"LOC-DEFAULT","reason":"CORRECTION","items":[{"product_code":"`+code+`","quantity":"2","direction":"IN","unit_symbol":""}]}`,
		"adjustment_number", http.StatusCreated)

	var drafts struct {
		Data struct {
			Items []struct {
				Number       string `json:"adjustment_number"`
				Kind         string `json:"adjustment_kind"`
				Status       string `json:"status"`
				LocationCode string `json:"location_code"`
			} `json:"items"`
		} `json:"data"`
	}
	getAndDecode(t, client, server.URL, "/api/inventory/adjustments?status=DRAFT", &drafts)
	if len(drafts.Data.Items) != 1 || drafts.Data.Items[0].Number != draft {
		t.Fatalf("expected the created draft in list, got %#v", drafts.Data.Items)
	}
	if drafts.Data.Items[0].Kind != "MANUAL" {
		t.Fatalf("expected MANUAL kind, got %q", drafts.Data.Items[0].Kind)
	}

	// Stok belum berubah sebelum adjustment selesai.
	if quantity := inventoryQuantity(t, client, server.URL, code); quantity != "10.0000" && quantity != "10" {
		t.Fatalf("draft must not change stock yet, got %q", quantity)
	}

	if status, body := postAndGetStatus(t, client, server.URL, csrf, http.MethodPost, "/api/inventory/adjustments/"+draft+"/complete", ""); status != http.StatusOK {
		t.Fatalf("complete failed, got %d. body: %s", status, body)
	}
	if quantity := inventoryQuantity(t, client, server.URL, code); quantity != "12.0000" && quantity != "12" {
		t.Fatalf("completed adjustment should raise stock to 12, got %q", quantity)
	}

	// Menyelesaikan adjustment yang sama dua kali harus ditolak.
	status, _ := postAndGetStatus(t, client, server.URL, csrf, http.MethodPost, "/api/inventory/adjustments/"+draft+"/complete", "")
	if status != http.StatusNotFound {
		t.Fatalf("expected 404 when completing twice, got %d", status)
	}
}

func TestSaleAndPurchaseReceiptUsePublicNumber(t *testing.T) {
	server, client, csrf := newTestServer(t, "User Receipt", "receipt@test.com", "Business Receipt")

	code := createProduct(t, client, server.URL, csrf, "Product Receipt", "")
	if status, body := postAndGetStatus(t, client, server.URL, csrf, http.MethodPost, "/api/inventory/opening-stock",
		`{"location_code":"LOC-DEFAULT","items":[{"product_code":"`+code+`","quantity":"20"}],"reason":""}`); !statusAllowed(status, []int{http.StatusOK, http.StatusCreated}) {
		t.Fatalf("opening stock failed, got %d. body: %s", status, body)
	}

	purchaseNumber := postIdempotent(t, client, server.URL, csrf, "/api/purchases",
		`{"location_code":"LOC-DEFAULT","supplier_code":"","discount_total":"0","tax_total":"0","items":[{"product_code":"`+code+`","quantity":"5","unit_price":"500","discount":"0"}]}`,
		"purchase_number", http.StatusCreated)

	var purchaseReceipt struct {
		Data struct {
			Receipt struct {
				PurchaseNumber string `json:"purchase_number"`
				Items          []struct {
					ProductCode string `json:"product_code"`
				} `json:"items"`
			} `json:"receipt"`
		} `json:"data"`
	}
	if status := getAndDecode(t, client, server.URL, "/api/purchases/"+purchaseNumber+"/receipt", &purchaseReceipt); status != http.StatusOK {
		t.Fatalf("purchase receipt status %d", status)
	}
	if purchaseReceipt.Data.Receipt.PurchaseNumber != purchaseNumber {
		t.Fatalf("receipt must expose public purchase number, got %q", purchaseReceipt.Data.Receipt.PurchaseNumber)
	}
	if len(purchaseReceipt.Data.Receipt.Items) != 1 || purchaseReceipt.Data.Receipt.Items[0].ProductCode != code {
		t.Fatalf("unexpected purchase receipt items: %#v", purchaseReceipt.Data.Receipt.Items)
	}

	saleNumber := postIdempotent(t, client, server.URL, csrf, "/api/sales",
		`{"location_code":"LOC-DEFAULT","customer_code":"","discount_total":"0","tax_total":"0","items":[{"product_code":"`+code+`","quantity":"2","unit_price":"1000","discount":"0"}]}`,
		"receipt_number", http.StatusCreated)

	var saleReceipt struct {
		Data struct {
			Receipt struct {
				ReceiptNumber string `json:"receipt_number"`
				GrandTotal    string `json:"grand_total"`
			} `json:"receipt"`
		} `json:"data"`
	}
	if status := getAndDecode(t, client, server.URL, "/api/sales/"+saleNumber+"/receipt", &saleReceipt); status != http.StatusOK {
		t.Fatalf("sale receipt status %d", status)
	}
	if saleReceipt.Data.Receipt.ReceiptNumber != saleNumber {
		t.Fatalf("receipt must expose public receipt number, got %q", saleReceipt.Data.Receipt.ReceiptNumber)
	}
	if saleReceipt.Data.Receipt.GrandTotal == "" {
		t.Fatal("receipt must expose grand total")
	}
}

func TestInventoryEndpointsRequireModuleAndRole(t *testing.T) {
	server, client, csrf := newTestServer(t, "User Guard", "guard@test.com", "Business Guard")

	// Modul inventory dimatikan lewat konfigurasi bisnis.
	status, body := postAndGetStatus(t, client, server.URL, csrf, http.MethodPut, "/api/businesses/current/configuration",
		`{"business_type":"RETAIL","enabled_modules":["CATALOG","SALES"]}`)
	if status != http.StatusOK {
		t.Fatalf("failed to update configuration, got %d. body: %s", status, body)
	}

	status, _ = postAndGetStatus(t, client, server.URL, csrf, http.MethodGet, "/api/inventory/products", "")
	if status != http.StatusForbidden {
		t.Fatalf("expected 403 when inventory module disabled, got %d", status)
	}
}

func TestAdjustmentRejectsUnknownUnitWithoutConversion(t *testing.T) {
	server, client, csrf := newTestServer(t, "User Unit", "unit@test.com", "Business Unit")

	code := createProduct(t, client, server.URL, csrf, "Product Unit", `"base_unit_symbol":"PCS"`)

	status, body := postAndGetStatus(t, client, server.URL, csrf, http.MethodPost, "/api/inventory/opening-stock",
		`{"location_code":"LOC-DEFAULT","items":[{"product_code":"`+code+`","quantity":"5","unit_symbol":"BOX"}],"reason":""}`)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404 for missing unit conversion, got %d. body: %s", status, body)
	}
	if !strings.Contains(body, "UNIT_CONVERSION_NOT_FOUND") {
		t.Fatalf("expected UNIT_CONVERSION_NOT_FOUND, got body: %s", body)
	}
}

func TestAdjustmentUsesUnitConversion(t *testing.T) {
	server, client, csrf := newTestServer(t, "User Conv", "conv@test.com", "Business Conv")

	// Produk dasar PCS (diberi saat onboarding), dengan konversi 1 BOX = 12 PCS.
	pcs := unitCodeBySymbol(t, client, server.URL, "PCS")
	box := postAndDecodeField(t, client, server.URL, csrf, http.MethodPost, "/api/units",
		`{"name":"Box","symbol":"BOX","unit_type":"COUNT"}`, "code", http.StatusCreated)

	code := createProduct(t, client, server.URL, csrf, "Product Conv", `"base_unit_symbol":"PCS"`)

	if status, body := postAndGetStatus(t, client, server.URL, csrf, http.MethodPost, "/api/unit-conversions",
		`{"product_code":"`+code+`","from_unit_code":"`+box+`","to_unit_code":"`+pcs+`","multiplier":"12"}`); !statusAllowed(status, []int{http.StatusOK, http.StatusCreated}) {
		t.Fatalf("failed to create unit conversion, got %d. body: %s", status, body)
	}

	if status, body := postAndGetStatus(t, client, server.URL, csrf, http.MethodPost, "/api/inventory/opening-stock",
		`{"location_code":"LOC-DEFAULT","items":[{"product_code":"`+code+`","quantity":"2","unit_symbol":"BOX"}],"reason":""}`); !statusAllowed(status, []int{http.StatusOK, http.StatusCreated}) {
		t.Fatalf("opening stock with conversion failed, got %d. body: %s", status, body)
	}

	// 2 BOX harus menjadi 24 pcs pada snapshot (snapshot selalu dalam unit dasar).
	if quantity := inventoryQuantity(t, client, server.URL, code); quantity != "24.0000" && quantity != "24" {
		t.Fatalf("expected 24 pcs in base unit, got %q", quantity)
	}
}

// TestStockLedgerCanBeRecalculated membuktikan DoD T12: seluruh perubahan stok
// dapat dijelaskan dari ledger stock_movements, dan penjumlahan ulang dari ledger
// harus sama persis dengan snapshot product_inventory.
//
// Semua movement wajib sudah selesai sebelum rekonsiliasi dijalankan, termasuk
// movement hasil opname.
func TestStockLedgerCanBeRecalculated(t *testing.T) {
	pool, handler := setupApp(t)
	server := httptest.NewServer(handler)
	defer server.Close()

	client := server.Client()
	csrf := registerUser(t, client, server.URL, "User Ledger", "ledger@test.com", "password")
	biz := createBusiness(t, client, server.URL, csrf, "Business Ledger")
	switchBusiness(t, client, server.URL, csrf, biz)

	first := createProduct(t, client, server.URL, csrf, "Ledger One", "")
	second := createProduct(t, client, server.URL, csrf, "Ledger Two", "")

	if status, body := postAndGetStatus(t, client, server.URL, csrf, http.MethodPost, "/api/inventory/opening-stock",
		`{"location_code":"LOC-DEFAULT","items":[`+
			`{"product_code":"`+first+`","quantity":"40"},`+
			`{"product_code":"`+second+`","quantity":"15"}],"reason":""}`); !statusAllowed(status, []int{http.StatusOK, http.StatusCreated}) {
		t.Fatalf("opening stock failed, got %d. body: %s", status, body)
	}

	// Penyesuaian IN pada produk pertama, OUT pada produk kedua.
	inbound := postAndDecodeField(t, client, server.URL, csrf, http.MethodPost, "/api/inventory/adjustments",
		`{"location_code":"LOC-DEFAULT","reason":"CORRECTION","items":[{"product_code":"`+first+`","quantity":"5","direction":"IN","unit_symbol":""}]}`,
		"adjustment_number", http.StatusCreated)
	outbound := postAndDecodeField(t, client, server.URL, csrf, http.MethodPost, "/api/inventory/adjustments",
		`{"location_code":"LOC-DEFAULT","reason":"LOST","items":[{"product_code":"`+second+`","quantity":"3","direction":"OUT","unit_symbol":""}]}`,
		"adjustment_number", http.StatusCreated)

	for _, number := range []string{inbound, outbound} {
		status, body := postIdempotentStatus(t, client, server.URL, csrf,
			"/api/inventory/adjustments/"+number+"/complete", "{}")
		if status != http.StatusOK {
			t.Fatalf("complete %s failed, got %d. body: %s", number, status, body)
		}
	}

	// Opname dibuat setelah adjustment selesai: sistem 45, hasil hitung fisik 38.
	opname := postAndDecodeField(t, client, server.URL, csrf, http.MethodPost, "/api/inventory/opnames",
		`{"location_code":"LOC-DEFAULT","items":[{"product_code":"`+first+`","counted_quantity":"38"}]}`,
		"adjustment_number", http.StatusCreated)
	status, body := postIdempotentStatus(t, client, server.URL, csrf,
		"/api/inventory/opnames/"+opname+"/complete", "{}")
	if status != http.StatusOK {
		t.Fatalf("opname %s failed, got %d. body: %s", opname, status, body)
	}

	// Nilai akhir harus sesuai ekspektasi skenario.
	if quantity := inventoryQuantity(t, client, server.URL, first); quantity != "38.0000" && quantity != "38" {
		t.Fatalf("first product should be 38 after opname, got %q", quantity)
	}
	if quantity := inventoryQuantity(t, client, server.URL, second); quantity != "12.0000" && quantity != "12" {
		t.Fatalf("second product should be 12 after OUT 3, got %q", quantity)
	}

	// Rekonsiliasi: penjumlahan seluruh movement dari ledger harus sama dengan snapshot.
	rows, err := pool.Query(context.Background(), `
		SELECT p.public_code, l.public_code,
		       COALESCE(ledger.total, 0)::text, COALESCE(pi.quantity, 0)::text
		FROM product_inventory pi
		JOIN products p ON p.id = pi.product_id AND p.business_id = pi.business_id
		JOIN locations l ON l.id = pi.location_id AND l.business_id = pi.business_id
		LEFT JOIN (
			SELECT sm.business_id, sm.product_id, sm.location_id,
			       SUM(CASE WHEN sm.direction = 'IN' THEN sm.base_quantity ELSE -sm.base_quantity END) AS total
			FROM stock_movements sm
			GROUP BY sm.business_id, sm.product_id, sm.location_id
		) ledger ON ledger.business_id = pi.business_id
			AND ledger.product_id = pi.product_id
			AND ledger.location_id = pi.location_id
		WHERE pi.business_id = (SELECT id FROM businesses WHERE public_code = $1)
		ORDER BY p.public_code`, biz)
	if err != nil {
		t.Fatalf("ledger query failed: %v", err)
	}
	defer rows.Close()

	seen := 0
	for rows.Next() {
		var productCode, locationCode, ledgerTotal, snapshot string
		if err := rows.Scan(&productCode, &locationCode, &ledgerTotal, &snapshot); err != nil {
			t.Fatalf("scan ledger row: %v", err)
		}
		seen++
		if ledgerTotal != snapshot {
			t.Fatalf("stok %s@%s tidak cocok: ledger=%s snapshot=%s", productCode, locationCode, ledgerTotal, snapshot)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate ledger rows: %v", err)
	}
	if seen == 0 {
		t.Fatal("expected at least one inventory row to reconcile")
	}
}
