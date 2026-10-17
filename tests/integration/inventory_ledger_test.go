//go:build integration

package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

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
