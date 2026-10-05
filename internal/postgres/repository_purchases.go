package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"usahainaja/backend/internal/app"
)

func (r *Repository) ListPurchases(ctx context.Context, businessID string, page, limit int) ([]app.Purchase, int, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 25
	}
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM purchases WHERE business_id = $1`, businessID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.pool.Query(ctx, `
		SELECT p.purchase_number, COALESCE(p.reference_number, ''), p.purchase_date, l.public_code, c.public_code, p.status, p.payment_status,
			   p.subtotal, p.discount_total, p.tax_total, p.grand_total, COALESCE(p.notes, '')
		FROM purchases p
		JOIN locations l ON l.id = p.location_id
		LEFT JOIN contacts c ON c.id = p.supplier_id
		WHERE p.business_id = $1
		ORDER BY p.purchase_date DESC LIMIT $2 OFFSET $3`, businessID, limit, (page-1)*limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]app.Purchase, 0)
	for rows.Next() {
		var item app.Purchase
		if err := rows.Scan(&item.PurchaseNumber, &item.ReferenceNumber, &item.PurchaseDate, &item.LocationCode, &item.SupplierCode,
			&item.Status, &item.PaymentStatus, &item.Subtotal, &item.DiscountTotal, &item.TaxTotal,
			&item.GrandTotal, &item.Notes); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (r *Repository) CreatePurchase(ctx context.Context, businessID, userID string, input app.NewPurchase) (app.Purchase, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return app.Purchase{}, err
	}
	defer rollback(ctx, tx)

	var locationID string
	err = tx.QueryRow(ctx, `SELECT id FROM locations WHERE business_id = $1 AND public_code = $2`, businessID, input.LocationCode).Scan(&locationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.Purchase{}, app.ErrNotFound
	}
	if err != nil {
		return app.Purchase{}, err
	}

	var supplierID *string
	if input.SupplierCode != "" {
		var cid string
		err = tx.QueryRow(ctx, `SELECT id FROM contacts WHERE business_id = $1 AND public_code = $2`, businessID, input.SupplierCode).Scan(&cid)
		if errors.Is(err, pgx.ErrNoRows) {
			return app.Purchase{}, app.ErrNotFound
		}
		if err != nil {
			return app.Purchase{}, err
		}
		supplierID = &cid
	}

	purchaseNumber, err := nextNumber(ctx, tx, businessID, "PURC")
	if err != nil {
		purchaseNumber = fmt.Sprintf("PU-%d", time.Now().UnixMilli())
	}

	var purchaseID string
	err = tx.QueryRow(ctx, `
		INSERT INTO purchases (
			business_id, location_id, supplier_id, purchase_number, reference_number, status, payment_status,
			discount_total, tax_total, created_by
		) VALUES ($1, $2, $3, $4, NULLIF($5, ''), 'DRAFT', 'UNPAID', $6, $7, $8) RETURNING id`,
		businessID, locationID, supplierID, purchaseNumber, input.ReferenceNumber, input.DiscountTotal, input.TaxTotal, userID,
	).Scan(&purchaseID)
	if err != nil {
		return app.Purchase{}, err
	}

	line := 0
	for _, item := range input.Items {
		line++
		var prodID string
		err = tx.QueryRow(ctx, `SELECT id FROM products WHERE business_id = $1 AND public_code = $2`, businessID, item.ProductCode).Scan(&prodID)
		if errors.Is(err, pgx.ErrNoRows) {
			return app.Purchase{}, app.ErrNotFound
		}
		if err != nil {
			return app.Purchase{}, err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO purchase_items (business_id, purchase_id, product_id, quantity, unit_price, discount, subtotal, line_number, ordered_quantity, received_quantity)
			VALUES ($1, $2, $3, $4, $5, $6, ($4::numeric * $5::numeric) - $6::numeric, $7, $4, 0)`,
			businessID, purchaseID, prodID, item.Quantity, item.UnitPrice, item.Discount, line,
		)
		if err != nil {
			return app.Purchase{}, err
		}
	}

	if err := recomputePurchaseTotals(ctx, tx, purchaseID); err != nil {
		return app.Purchase{}, err
	}
	if err := auditTx(ctx, tx, businessID, userID, "PURCHASE", purchaseID, purchaseNumber, "CREATE", ""); err != nil {
		return app.Purchase{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return app.Purchase{}, err
	}

	return r.GetPurchase(ctx, businessID, purchaseNumber)
}

func recomputePurchaseTotals(ctx context.Context, tx pgx.Tx, purchaseID string) error {
	_, err := tx.Exec(ctx, `
		UPDATE purchases
		SET subtotal = (SELECT COALESCE(SUM(subtotal), 0) FROM purchase_items WHERE purchase_id = $1),
			grand_total = (SELECT COALESCE(SUM(subtotal), 0) FROM purchase_items WHERE purchase_id = $1) - discount_total + tax_total,
			updated_at = now()
		WHERE id = $1`, purchaseID)
	return err
}

func (r *Repository) GetPurchase(ctx context.Context, businessID, purchaseNumber string) (app.Purchase, error) {
	var p app.Purchase
	var purchaseID string
	err := r.pool.QueryRow(ctx, `
		SELECT p.id, p.purchase_number, COALESCE(p.reference_number, ''), p.purchase_date, l.public_code, c.public_code,
		       p.status, p.payment_status, p.subtotal, p.discount_total, p.tax_total, p.grand_total, COALESCE(p.notes, '')
		FROM purchases p
		JOIN locations l ON l.id = p.location_id
		LEFT JOIN contacts c ON c.id = p.supplier_id
		WHERE p.business_id = $1 AND p.purchase_number = $2`, businessID, purchaseNumber,
	).Scan(&purchaseID, &p.PurchaseNumber, &p.ReferenceNumber, &p.PurchaseDate, &p.LocationCode, &p.SupplierCode,
		&p.Status, &p.PaymentStatus, &p.Subtotal, &p.DiscountTotal, &p.TaxTotal, &p.GrandTotal, &p.Notes)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.Purchase{}, app.ErrNotFound
	}
	if err != nil {
		return app.Purchase{}, err
	}

	rows, err := r.pool.Query(ctx, `
		SELECT pi.line_number, pr.public_code, pr.name, pi.quantity, pi.ordered_quantity, pi.received_quantity,
		       pi.unit_price, pi.discount, pi.subtotal, COALESCE(pi.notes, '')
		FROM purchase_items pi
		JOIN products pr ON pr.id = pi.product_id
		WHERE pi.business_id = $1 AND pi.purchase_id = $2
		ORDER BY pi.line_number`, businessID, purchaseID)
	if err != nil {
		return app.Purchase{}, err
	}
	defer rows.Close()
	p.Items = make([]app.PurchaseItem, 0)
	for rows.Next() {
		var it app.PurchaseItem
		if err := rows.Scan(&it.LineNumber, &it.ProductCode, &it.ProductName, &it.Quantity,
			&it.OrderedQuantity, &it.ReceivedQuantity, &it.UnitPrice, &it.Discount, &it.Subtotal, &it.Notes); err != nil {
			return app.Purchase{}, err
		}
		p.Items = append(p.Items, it)
	}
	if err := rows.Err(); err != nil {
		return app.Purchase{}, err
	}

	rrows, err := r.pool.Query(ctx, `
		SELECT receipt_number, received_at, COALESCE(reference_number, '')
		FROM purchase_receipts
		WHERE business_id = $1 AND purchase_id = $2
		ORDER BY received_at`, businessID, purchaseID)
	if err != nil {
		return app.Purchase{}, err
	}
	defer rrows.Close()
	p.Receipts = make([]app.PurchaseReceipt, 0)
	for rrows.Next() {
		var rc app.PurchaseReceipt
		if err := rrows.Scan(&rc.ReceiptNumber, &rc.ReceivedAt, &rc.ReferenceNumber); err != nil {
			return app.Purchase{}, err
		}
		irows, err := r.pool.Query(ctx, `
			SELECT ri.line_number, pr.public_code, ri.received_quantity
			FROM purchase_receipt_items ri
			JOIN products pr ON pr.id = ri.product_id
			JOIN purchase_receipts r ON r.business_id = ri.business_id AND r.id = ri.purchase_receipt_id
			WHERE ri.business_id = $1 AND r.receipt_number = $2
			ORDER BY ri.line_number`, businessID, rc.ReceiptNumber)
		if err != nil {
			return app.Purchase{}, err
		}
		rc.Items = make([]app.PurchaseReceiptItem, 0)
		for irows.Next() {
			var ri app.PurchaseReceiptItem
			if err := irows.Scan(&ri.LineNumber, &ri.ProductCode, &ri.ReceivedQuantity); err != nil {
				irows.Close()
				return app.Purchase{}, err
			}
			rc.Items = append(rc.Items, ri)
		}
		irows.Close()
		if err := irows.Err(); err != nil {
			return app.Purchase{}, err
		}
		p.Receipts = append(p.Receipts, rc)
	}
	if err := rrows.Err(); err != nil {
		return app.Purchase{}, err
	}

	paid, err := purchasePaidTotal(ctx, r.pool, businessID, purchaseID)
	if err != nil {
		return app.Purchase{}, err
	}
	p.PaidTotal = paid
	outstanding, err := subtractDecimal(p.GrandTotal, paid)
	if err != nil {
		return app.Purchase{}, err
	}
	p.OutstandingTotal = outstanding
	return p, nil
}

func (r *Repository) AddPurchaseItem(ctx context.Context, businessID, userID, purchaseNumber string, item app.NewPurchaseItem) (app.PurchaseItem, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return app.PurchaseItem{}, err
	}
	defer rollback(ctx, tx)

	var purchaseID, status string
	err = tx.QueryRow(ctx, `SELECT id, status FROM purchases WHERE business_id = $1 AND purchase_number = $2 FOR UPDATE`, businessID, purchaseNumber).Scan(&purchaseID, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.PurchaseItem{}, app.ErrNotFound
	}
	if err != nil {
		return app.PurchaseItem{}, err
	}
	if status != "DRAFT" {
		return app.PurchaseItem{}, &app.Error{Code: "INVALID_STATE", Message: "Item hanya dapat ditambah pada purchase DRAFT."}
	}

	var prodID, prodName string
	err = tx.QueryRow(ctx, `SELECT id, name FROM products WHERE business_id = $1 AND public_code = $2`, businessID, item.ProductCode).Scan(&prodID, &prodName)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.PurchaseItem{}, app.ErrNotFound
	}
	if err != nil {
		return app.PurchaseItem{}, err
	}

	var line int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(line_number), 0) + 1 FROM purchase_items WHERE business_id = $1 AND purchase_id = $2`, businessID, purchaseID).Scan(&line); err != nil {
		return app.PurchaseItem{}, err
	}
	var subtotal string
	if err := tx.QueryRow(ctx, `SELECT (($1::numeric * $2::numeric) - $3::numeric)::text`, item.Quantity, item.UnitPrice, item.Discount).Scan(&subtotal); err != nil {
		return app.PurchaseItem{}, err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO purchase_items (business_id, purchase_id, product_id, quantity, unit_price, discount, subtotal, line_number, ordered_quantity, received_quantity, notes)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $4, 0, NULLIF($9, ''))`,
		businessID, purchaseID, prodID, item.Quantity, item.UnitPrice, item.Discount, subtotal, line, item.Notes)
	if err != nil {
		return app.PurchaseItem{}, err
	}
	if err := recomputePurchaseTotals(ctx, tx, purchaseID); err != nil {
		return app.PurchaseItem{}, err
	}
	if err := auditTx(ctx, tx, businessID, userID, "PURCHASE", purchaseID, purchaseNumber, "ITEM_ADDED", ""); err != nil {
		return app.PurchaseItem{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return app.PurchaseItem{}, err
	}
	return app.PurchaseItem{LineNumber: line, ProductCode: item.ProductCode, ProductName: prodName,
		Quantity: item.Quantity, OrderedQuantity: item.Quantity, ReceivedQuantity: "0",
		UnitPrice: item.UnitPrice, Discount: item.Discount, Subtotal: subtotal, Notes: item.Notes}, nil
}

func (r *Repository) UpdatePurchaseItem(ctx context.Context, businessID, userID, purchaseNumber string, lineNumber int, item app.NewPurchaseItem) (app.PurchaseItem, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return app.PurchaseItem{}, err
	}
	defer rollback(ctx, tx)

	var purchaseID, status string
	err = tx.QueryRow(ctx, `SELECT id, status FROM purchases WHERE business_id = $1 AND purchase_number = $2 FOR UPDATE`, businessID, purchaseNumber).Scan(&purchaseID, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.PurchaseItem{}, app.ErrNotFound
	}
	if err != nil {
		return app.PurchaseItem{}, err
	}
	if status != "DRAFT" && status != "ORDERED" && status != "PARTIALLY_RECEIVED" {
		return app.PurchaseItem{}, &app.Error{Code: "INVALID_STATE", Message: "Item tidak dapat diubah pada status ini."}
	}

	var itemID, received string
	err = tx.QueryRow(ctx, `SELECT id, received_quantity::text FROM purchase_items WHERE business_id = $1 AND purchase_id = $2 AND line_number = $3 FOR UPDATE`, businessID, purchaseID, lineNumber).Scan(&itemID, &received)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.PurchaseItem{}, app.ErrNotFound
	}
	if err != nil {
		return app.PurchaseItem{}, err
	}
	if received != "0" && !isZeroDecimal(received) {
		return app.PurchaseItem{}, &app.Error{Code: "INVALID_STATE", Message: "Item yang sudah diterima tidak dapat diubah."}
	}

	productCode := item.ProductCode
	var prodID, prodName string
	if productCode == "" {
		err = tx.QueryRow(ctx, `SELECT p.public_code, p.name FROM products p JOIN purchase_items pi ON pi.product_id = p.id WHERE pi.id = $1`, itemID).Scan(&productCode, &prodName)
		if err != nil {
			return app.PurchaseItem{}, err
		}
		err = tx.QueryRow(ctx, `SELECT id FROM products WHERE business_id = $1 AND public_code = $2`, businessID, productCode).Scan(&prodID)
		if err != nil {
			return app.PurchaseItem{}, err
		}
	} else {
		err = tx.QueryRow(ctx, `SELECT id, name FROM products WHERE business_id = $1 AND public_code = $2`, businessID, productCode).Scan(&prodID, &prodName)
		if errors.Is(err, pgx.ErrNoRows) {
			return app.PurchaseItem{}, app.ErrNotFound
		}
		if err != nil {
			return app.PurchaseItem{}, err
		}
	}

	var subtotal string
	if err := tx.QueryRow(ctx, `SELECT (($1::numeric * $2::numeric) - $3::numeric)::text`, item.Quantity, item.UnitPrice, item.Discount).Scan(&subtotal); err != nil {
		return app.PurchaseItem{}, err
	}
	_, err = tx.Exec(ctx, `
		UPDATE purchase_items SET product_id = $1, quantity = $2, ordered_quantity = $2, unit_price = $3, discount = $4, subtotal = $5, notes = NULLIF($6, '')
		WHERE id = $7`, prodID, item.Quantity, item.UnitPrice, item.Discount, subtotal, item.Notes, itemID)
	if err != nil {
		return app.PurchaseItem{}, err
	}
	if err := recomputePurchaseTotals(ctx, tx, purchaseID); err != nil {
		return app.PurchaseItem{}, err
	}
	if err := auditTx(ctx, tx, businessID, userID, "PURCHASE", purchaseID, purchaseNumber, "ITEM_UPDATED", ""); err != nil {
		return app.PurchaseItem{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return app.PurchaseItem{}, err
	}
	return app.PurchaseItem{LineNumber: lineNumber, ProductCode: productCode, ProductName: prodName,
		Quantity: item.Quantity, OrderedQuantity: item.Quantity, ReceivedQuantity: received,
		UnitPrice: item.UnitPrice, Discount: item.Discount, Subtotal: subtotal, Notes: item.Notes}, nil
}

func (r *Repository) DeletePurchaseItem(ctx context.Context, businessID, userID, purchaseNumber string, lineNumber int) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer rollback(ctx, tx)

	var purchaseID, status string
	err = tx.QueryRow(ctx, `SELECT id, status FROM purchases WHERE business_id = $1 AND purchase_number = $2 FOR UPDATE`, businessID, purchaseNumber).Scan(&purchaseID, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.ErrNotFound
	}
	if err != nil {
		return err
	}
	if status != "DRAFT" && status != "ORDERED" && status != "PARTIALLY_RECEIVED" {
		return &app.Error{Code: "INVALID_STATE", Message: "Item tidak dapat dihapus pada status ini."}
	}
	var received string
	err = tx.QueryRow(ctx, `SELECT received_quantity::text FROM purchase_items WHERE business_id = $1 AND purchase_id = $2 AND line_number = $3`, businessID, purchaseID, lineNumber).Scan(&received)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.ErrNotFound
	}
	if err != nil {
		return err
	}
	if !isZeroDecimal(received) {
		return &app.Error{Code: "INVALID_STATE", Message: "Item yang sudah diterima tidak dapat dihapus."}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM purchase_items WHERE business_id = $1 AND purchase_id = $2 AND line_number = $3`, businessID, purchaseID, lineNumber); err != nil {
		return err
	}
	if err := recomputePurchaseTotals(ctx, tx, purchaseID); err != nil {
		return err
	}
	if err := auditTx(ctx, tx, businessID, userID, "PURCHASE", purchaseID, purchaseNumber, "ITEM_DELETED", ""); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Repository) OrderPurchase(ctx context.Context, businessID, userID, purchaseNumber string) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer rollback(ctx, tx)

	var purchaseID, status string
	var lines int
	err = tx.QueryRow(ctx, `
		SELECT p.id, p.status, (SELECT COUNT(*) FROM purchase_items pi WHERE pi.purchase_id = p.id)
		FROM purchases p WHERE p.business_id = $1 AND p.purchase_number = $2 FOR UPDATE`, businessID, purchaseNumber).Scan(&purchaseID, &status, &lines)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.ErrNotFound
	}
	if err != nil {
		return err
	}
	if status != "DRAFT" {
		return &app.Error{Code: "INVALID_STATE", Message: "Hanya purchase DRAFT yang dapat diorder."}
	}
	if lines == 0 {
		return &app.Error{Code: "INVALID_STATE", Message: "Purchase tanpa item tidak dapat diorder."}
	}
	if _, err := tx.Exec(ctx, `UPDATE purchases SET status = 'ORDERED', updated_at = now() WHERE id = $1`, purchaseID); err != nil {
		return err
	}
	if err := auditTx(ctx, tx, businessID, userID, "PURCHASE", purchaseID, purchaseNumber, "ORDERED", ""); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Repository) CancelPurchase(ctx context.Context, businessID, userID, purchaseNumber string) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer rollback(ctx, tx)

	var purchaseID, status string
	err = tx.QueryRow(ctx, `SELECT id, status FROM purchases WHERE business_id = $1 AND purchase_number = $2 FOR UPDATE`, businessID, purchaseNumber).Scan(&purchaseID, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.ErrNotFound
	}
	if err != nil {
		return err
	}
	if status != "DRAFT" && status != "ORDERED" {
		return &app.Error{Code: "INVALID_STATE", Message: "Hanya purchase DRAFT/ORDERED yang dapat dibatalkan."}
	}
	var receipts, payments int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM purchase_receipts WHERE purchase_id = $1`, purchaseID).Scan(&receipts); err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM payments WHERE purchase_id = $1`, purchaseID).Scan(&payments); err != nil {
		return err
	}
	if receipts > 0 || payments > 0 {
		return &app.Error{Code: "INVALID_STATE", Message: "Purchase dengan receipt/payment tidak dapat dibatalkan."}
	}
	if _, err := tx.Exec(ctx, `UPDATE purchases SET status = 'CANCELLED', updated_at = now() WHERE id = $1`, purchaseID); err != nil {
		return err
	}
	if err := auditTx(ctx, tx, businessID, userID, "PURCHASE", purchaseID, purchaseNumber, "CANCELLED", ""); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type receiveLine struct {
	itemID    string
	line      int
	productID string
	baseUnit  string
	ordered   string
	received  string
	newQty    string
}

func (r *Repository) ReceivePurchase(ctx context.Context, businessID, purchaseNumber, userID string, in app.ReceivePurchaseInput) (app.PurchaseReceipt, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return app.PurchaseReceipt{}, err
	}
	defer rollback(ctx, tx)

	var purchaseID, locationID, status string
	err = tx.QueryRow(ctx, `SELECT id, location_id, status FROM purchases WHERE business_id = $1 AND purchase_number = $2 FOR UPDATE`, businessID, purchaseNumber).Scan(&purchaseID, &locationID, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.PurchaseReceipt{}, app.ErrNotFound
	}
	if err != nil {
		return app.PurchaseReceipt{}, err
	}
	if status != "DRAFT" && status != "ORDERED" && status != "PARTIALLY_RECEIVED" {
		return app.PurchaseReceipt{}, &app.Error{Code: "INVALID_STATE", Message: "Purchase pada status ini tidak dapat diterima."}
	}
	if len(in.Items) == 0 {
		return app.PurchaseReceipt{}, &app.Error{Code: "VALIDATION_ERROR", Message: "Periksa kembali data yang dikirim.", Cause: errors.New("items required")}
	}

	receivedAt := time.Now()
	if in.ReceivedAt != "" {
		if parsed, perr := time.Parse(time.RFC3339, in.ReceivedAt); perr == nil {
			receivedAt = parsed
		}
	}

	lines := make([]receiveLine, 0, len(in.Items))
	seen := map[int]bool{}
	for _, it := range in.Items {
		if seen[it.LineNumber] {
			return app.PurchaseReceipt{}, &app.Error{Code: "VALIDATION_ERROR", Message: "Periksa kembali data yang dikirim.", Cause: errors.New("duplicate line")}
		}
		seen[it.LineNumber] = true
		var ln receiveLine
		err = tx.QueryRow(ctx, `
			SELECT pi.id, pi.line_number, pi.product_id, p.base_unit_id, pi.ordered_quantity::text, pi.received_quantity::text
			FROM purchase_items pi JOIN products p ON p.id = pi.product_id
			WHERE pi.business_id = $1 AND pi.purchase_id = $2 AND pi.line_number = $3 FOR UPDATE`,
			businessID, purchaseID, it.LineNumber).Scan(&ln.itemID, &ln.line, &ln.productID, &ln.baseUnit, &ln.ordered, &ln.received)
		if errors.Is(err, pgx.ErrNoRows) {
			return app.PurchaseReceipt{}, app.ErrNotFound
		}
		if err != nil {
			return app.PurchaseReceipt{}, err
		}
		remaining, err := subtractDecimal(ln.ordered, ln.received)
		if err != nil {
			return app.PurchaseReceipt{}, err
		}
		ok, err := decimalLTE(it.ReceivedQuantity, remaining)
		if err != nil || !ok {
			return app.PurchaseReceipt{}, &app.Error{Code: "OVER_RECEIPT", Message: "Jumlah diterima melebihi sisa pesanan."}
		}
		positive, err := decimalPositive(it.ReceivedQuantity)
		if err != nil || !positive {
			return app.PurchaseReceipt{}, &app.Error{Code: "VALIDATION_ERROR", Message: "Periksa kembali data yang dikirim.", Cause: errors.New("received_quantity must be positive")}
		}
		ln.newQty = it.ReceivedQuantity
		lines = append(lines, ln)
	}

	receiptNumber, err := nextNumber(ctx, tx, businessID, "PURCHASE_RECEIPT")
	if err != nil {
		receiptNumber = fmt.Sprintf("RCV-%d", time.Now().UnixMilli())
	}
	var receiptID string
	err = tx.QueryRow(ctx, `
		INSERT INTO purchase_receipts (business_id, purchase_id, location_id, receipt_number, received_at, reference_number, created_by)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7) RETURNING id`,
		businessID, purchaseID, locationID, receiptNumber, receivedAt, in.ReferenceNumber, userID).Scan(&receiptID)
	if err != nil {
		return app.PurchaseReceipt{}, err
	}

	receipt := app.PurchaseReceipt{ReceiptNumber: receiptNumber, ReceivedAt: receivedAt, ReferenceNumber: in.ReferenceNumber}
	for _, ln := range lines {
		var prodCode string
		if err := tx.QueryRow(ctx, `SELECT public_code FROM products WHERE id = $1`, ln.productID).Scan(&prodCode); err != nil {
			return app.PurchaseReceipt{}, err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO purchase_receipt_items (business_id, purchase_receipt_id, purchase_item_id, product_id, line_number, received_quantity)
			SELECT $1, $2, pi.id, pi.product_id, pi.line_number, $3::numeric
			FROM purchase_items pi WHERE pi.id = $4`,
			businessID, receiptID, ln.newQty, ln.itemID); err != nil {
			return app.PurchaseReceipt{}, err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE purchase_items SET received_quantity = received_quantity + $1::numeric WHERE id = $2`, ln.newQty, ln.itemID); err != nil {
			return app.PurchaseReceipt{}, err
		}
		// tracked check for snapshot insert
		var tracked bool
		if err := tx.QueryRow(ctx, `SELECT is_stock_tracked FROM products WHERE id = $1`, ln.productID).Scan(&tracked); err != nil {
			return app.PurchaseReceipt{}, err
		}
		if !tracked {
			continue
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO stock_movements (business_id, product_id, location_id, movement_type, direction, quantity, unit_id, base_quantity, base_unit_id, purchase_receipt_id, created_by)
			VALUES ($1, $2, $3, 'PURCHASE', 'IN', $4, $5, $4, $5, $6, $7)`,
			businessID, ln.productID, locationID, ln.newQty, ln.baseUnit, receiptID, userID); err != nil {
			return app.PurchaseReceipt{}, err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO product_inventory (business_id, product_id, location_id, quantity, base_unit_id)
			VALUES ($1, $2, $3, $4::numeric, $5)
			ON CONFLICT (business_id, product_id, location_id) DO UPDATE SET quantity = product_inventory.quantity + $4::numeric, updated_at = now()`,
			businessID, ln.productID, locationID, ln.newQty, ln.baseUnit); err != nil {
			return app.PurchaseReceipt{}, err
		}
		receipt.Items = append(receipt.Items, app.PurchaseReceiptItem{LineNumber: ln.line, ProductCode: prodCode, ReceivedQuantity: ln.newQty})
	}

	var remaining int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM purchase_items WHERE purchase_id = $1 AND received_quantity < ordered_quantity`, purchaseID).Scan(&remaining); err != nil {
		return app.PurchaseReceipt{}, err
	}
	newStatus := "PARTIALLY_RECEIVED"
	if remaining == 0 {
		newStatus = "RECEIVED"
	}
	if _, err := tx.Exec(ctx, `UPDATE purchases SET status = $1, updated_at = now() WHERE id = $2`, newStatus, purchaseID); err != nil {
		return app.PurchaseReceipt{}, err
	}
	if err := auditTx(ctx, tx, businessID, userID, "PURCHASE", purchaseID, purchaseNumber, "RECEIVED", in.ReferenceNumber); err != nil {
		return app.PurchaseReceipt{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return app.PurchaseReceipt{}, err
	}
	return receipt, nil
}

func (r *Repository) RecordPurchasePayment(ctx context.Context, businessID, purchaseNumber, userID string, in app.PaymentInput) (app.Payment, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return app.Payment{}, err
	}
	defer rollback(ctx, tx)

	var cashID, cashCode string
	err = tx.QueryRow(ctx, `SELECT id, public_code FROM cash_accounts WHERE business_id = $1 AND public_code = $2 AND status = 'ACTIVE'`, businessID, in.CashAccountCode).Scan(&cashID, &cashCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.Payment{}, app.ErrNotFound
	}
	if err != nil {
		return app.Payment{}, err
	}

	var purchaseID, status, grand string
	err = tx.QueryRow(ctx, `SELECT id, status, grand_total::text FROM purchases WHERE business_id = $1 AND purchase_number = $2 FOR UPDATE`, businessID, purchaseNumber).Scan(&purchaseID, &status, &grand)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.Payment{}, app.ErrNotFound
	}
	if err != nil {
		return app.Payment{}, err
	}
	if status != "PARTIALLY_RECEIVED" && status != "RECEIVED" {
		return app.Payment{}, &app.Error{Code: "INVALID_STATE", Message: "Pembayaran hanya untuk purchase yang sudah diterima."}
	}

	paid, err := purchasePaidTotalTx(ctx, tx, businessID, purchaseID)
	if err != nil {
		return app.Payment{}, err
	}
	outstanding, err := subtractDecimal(grand, paid)
	if err != nil {
		return app.Payment{}, err
	}
	ok, err := decimalLTE(in.Amount, outstanding)
	if err != nil {
		return app.Payment{}, err
	}
	if !ok {
		return app.Payment{}, &app.Error{Code: "OVERPAYMENT", Message: "Pembayaran melebihi sisa tagihan."}
	}

	method := in.PaymentMethod
	if method == "" {
		method = "CASH"
	}
	paidAt := time.Now()
	if in.PaidAt != "" {
		if parsed, perr := time.Parse(time.RFC3339, in.PaidAt); perr == nil {
			paidAt = parsed
		}
	}

	payNumber, err := nextNumber(ctx, tx, businessID, "PAY")
	if err != nil || payNumber == "" {
		payNumber = fmt.Sprintf("PAY-%d", time.Now().UnixMilli())
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO payments (business_id, cash_account_id, purchase_id, payment_number, payment_date, payment_method, amount, kind, reference_number, notes, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'CHARGE', NULLIF($8, ''), NULLIF($9, ''), $10)`,
		businessID, cashID, purchaseID, payNumber, paidAt, method, in.Amount, in.ReferenceNumber, in.Notes, userID)
	if err != nil {
		return app.Payment{}, err
	}

	if _, err := tx.Exec(ctx, `UPDATE cash_accounts SET balance = balance - $1::numeric, updated_at = now() WHERE id = $2`, in.Amount, cashID); err != nil {
		return app.Payment{}, err
	}

	newPaid, err := addDecimal(paid, in.Amount)
	if err != nil {
		return app.Payment{}, err
	}
	newStatus := "PARTIAL"
	cmp, err := compareDecimal(newPaid, grand)
	if err != nil {
		return app.Payment{}, err
	}
	switch {
	case cmp == 0:
		newStatus = "PAID"
	case isZeroDecimal(newPaid):
		newStatus = "UNPAID"
	case cmp > 0:
		return app.Payment{}, &app.Error{Code: "OVERPAYMENT", Message: "Pembayaran melebihi sisa tagihan."}
	}

	_, err = tx.Exec(ctx, `UPDATE purchases SET payment_status = $1, updated_at = now() WHERE id = $2`, newStatus, purchaseID)
	if err != nil {
		return app.Payment{}, err
	}
	if err := auditTx(ctx, tx, businessID, userID, "PURCHASE", purchaseID, purchaseNumber, "PAYMENT_RECORDED", in.ReferenceNumber); err != nil {
		return app.Payment{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return app.Payment{}, err
	}

	newOutstanding, _ := subtractDecimal(grand, newPaid)
	return app.Payment{
		PaymentNumber: payNumber, CashAccountCode: cashCode, PaymentDate: paidAt,
		Amount: in.Amount, Kind: "CHARGE", PaidTotal: newPaid, OutstandingTotal: newOutstanding,
		ReferenceNumber: in.ReferenceNumber, Notes: in.Notes,
	}, nil
}
