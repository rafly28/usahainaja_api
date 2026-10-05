package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"usahainaja/backend/internal/app"
)

func (r *Repository) ListSales(ctx context.Context, businessID string, page, limit int) ([]app.Sale, int, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 25
	}
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM sales WHERE business_id = $1`, businessID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.pool.Query(ctx, `
		SELECT s.receipt_number, s.sale_date, l.public_code, c.public_code, s.status, s.payment_status,
			   s.subtotal, s.discount_total, s.tax_total, s.grand_total, COALESCE(s.notes, '')
		FROM sales s
		JOIN locations l ON l.id = s.location_id
		LEFT JOIN contacts c ON c.id = s.customer_id
		WHERE s.business_id = $1
		ORDER BY s.sale_date DESC LIMIT $2 OFFSET $3`, businessID, limit, (page-1)*limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]app.Sale, 0)
	for rows.Next() {
		var item app.Sale
		if err := rows.Scan(&item.ReceiptNumber, &item.SaleDate, &item.LocationCode, &item.CustomerCode,
			&item.Status, &item.PaymentStatus, &item.Subtotal, &item.DiscountTotal, &item.TaxTotal,
			&item.GrandTotal, &item.Notes); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (r *Repository) CreateSale(ctx context.Context, businessID, userID string, input app.NewSale) (app.Sale, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return app.Sale{}, err
	}
	defer rollback(ctx, tx)

	var locationID string
	err = tx.QueryRow(ctx, `SELECT id FROM locations WHERE business_id = $1 AND public_code = $2`, businessID, input.LocationCode).Scan(&locationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.Sale{}, app.ErrNotFound
	}
	if err != nil {
		return app.Sale{}, err
	}

	var customerID *string
	if input.CustomerCode != "" {
		var cid string
		err = tx.QueryRow(ctx, `SELECT id FROM contacts WHERE business_id = $1 AND public_code = $2 AND contact_type IN ('CUSTOMER', 'BOTH')`, businessID, input.CustomerCode).Scan(&cid)
		if err != nil {
			return app.Sale{}, &app.Error{Code: "NOT_FOUND", Message: "Pelanggan tidak ditemukan"}
		}
		customerID = &cid
	}

	receiptNumber, err := nextNumber(ctx, tx, businessID, "SALE")
	if err != nil {
		receiptNumber = fmt.Sprintf("SL-%d", time.Now().UnixMilli())
	}

	var saleID string
	err = tx.QueryRow(ctx, `
		INSERT INTO sales (
			business_id, location_id, customer_id, receipt_number, status, payment_status,
			discount_total, tax_total, notes, created_by
		) VALUES ($1, $2, $3, $4, 'DRAFT', 'UNPAID', $5, $6, NULLIF($7, ''), $8) RETURNING id`,
		businessID, locationID, customerID, receiptNumber, input.DiscountTotal, input.TaxTotal, input.Notes, userID,
	).Scan(&saleID)
	if err != nil {
		return app.Sale{}, err
	}

	line := 0
	for _, item := range input.Items {
		line++
		var prodID string
		err = tx.QueryRow(ctx, `SELECT id FROM products WHERE business_id = $1 AND public_code = $2`, businessID, item.ProductCode).Scan(&prodID)
		if errors.Is(err, pgx.ErrNoRows) {
			return app.Sale{}, app.ErrNotFound
		}
		if err != nil {
			return app.Sale{}, err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO sale_items (business_id, sale_id, product_id, quantity, unit_price, discount, subtotal, line_number, notes)
			VALUES ($1, $2, $3, $4, $5, $6, ($4::numeric * $5::numeric) - $6::numeric, $7, NULLIF($8, ''))`,
			businessID, saleID, prodID, item.Quantity, item.UnitPrice, item.Discount, line, item.Notes,
		)
		if err != nil {
			return app.Sale{}, err
		}
	}

	if err := recomputeSaleTotals(ctx, tx, saleID); err != nil {
		return app.Sale{}, err
	}
	if err := auditTx(ctx, tx, businessID, userID, "SALE", saleID, receiptNumber, "CREATE", ""); err != nil {
		return app.Sale{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return app.Sale{}, err
	}

	return r.GetSale(ctx, businessID, receiptNumber)
}

func recomputeSaleTotals(ctx context.Context, tx pgx.Tx, saleID string) error {
	_, err := tx.Exec(ctx, `
		UPDATE sales
		SET subtotal = (SELECT COALESCE(SUM(subtotal), 0) FROM sale_items WHERE sale_id = $1),
			grand_total = (SELECT COALESCE(SUM(subtotal), 0) FROM sale_items WHERE sale_id = $1) - discount_total + tax_total,
			updated_at = now()
		WHERE id = $1`, saleID)
	return err
}

func (r *Repository) GetSale(ctx context.Context, businessID, receiptNumber string) (app.Sale, error) {
	var s app.Sale
	var saleID string
	err := r.pool.QueryRow(ctx, `
		SELECT s.id, s.receipt_number, s.sale_date, l.public_code, c.public_code, s.status, s.payment_status,
		       s.subtotal, s.discount_total, s.tax_total, s.grand_total, COALESCE(s.notes, '')
		FROM sales s
		JOIN locations l ON l.id = s.location_id
		LEFT JOIN contacts c ON c.id = s.customer_id
		WHERE s.business_id = $1 AND s.receipt_number = $2`, businessID, receiptNumber,
	).Scan(&saleID, &s.ReceiptNumber, &s.SaleDate, &s.LocationCode, &s.CustomerCode,
		&s.Status, &s.PaymentStatus, &s.Subtotal, &s.DiscountTotal, &s.TaxTotal, &s.GrandTotal, &s.Notes)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.Sale{}, app.ErrNotFound
	}
	if err != nil {
		return app.Sale{}, err
	}
	rows, err := r.pool.Query(ctx, `
		SELECT si.line_number, pr.public_code, pr.name, si.quantity, si.unit_price, si.discount, si.subtotal, COALESCE(si.notes, '')
		FROM sale_items si JOIN products pr ON pr.id = si.product_id
		WHERE si.business_id = $1 AND si.sale_id = $2 ORDER BY si.line_number`, businessID, saleID)
	if err != nil {
		return app.Sale{}, err
	}
	defer rows.Close()
	s.Items = make([]app.SaleItem, 0)
	for rows.Next() {
		var it app.SaleItem
		if err := rows.Scan(&it.LineNumber, &it.ProductCode, &it.ProductName, &it.Quantity,
			&it.UnitPrice, &it.Discount, &it.Subtotal, &it.Notes); err != nil {
			return app.Sale{}, err
		}
		s.Items = append(s.Items, it)
	}
	if err := rows.Err(); err != nil {
		return app.Sale{}, err
	}
	paid, err := salePaidTotalTxPool(ctx, r.pool, businessID, saleID)
	if err != nil {
		return app.Sale{}, err
	}
	s.PaidTotal = paid
	return s, nil
}

func (r *Repository) AddSaleItem(ctx context.Context, businessID, userID, receiptNumber string, item app.NewSaleItem) (app.SaleItem, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return app.SaleItem{}, err
	}
	defer rollback(ctx, tx)

	var saleID, status string
	err = tx.QueryRow(ctx, `SELECT id, status FROM sales WHERE business_id = $1 AND receipt_number = $2 FOR UPDATE`, businessID, receiptNumber).Scan(&saleID, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.SaleItem{}, app.ErrNotFound
	}
	if err != nil {
		return app.SaleItem{}, err
	}
	if status != "DRAFT" {
		return app.SaleItem{}, &app.Error{Code: "INVALID_STATE", Message: "Item hanya dapat ditambah pada sale DRAFT."}
	}
	var prodID, prodName string
	err = tx.QueryRow(ctx, `SELECT id, name FROM products WHERE business_id = $1 AND public_code = $2`, businessID, item.ProductCode).Scan(&prodID, &prodName)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.SaleItem{}, app.ErrNotFound
	}
	if err != nil {
		return app.SaleItem{}, err
	}
	var line int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(line_number), 0) + 1 FROM sale_items WHERE business_id = $1 AND sale_id = $2`, businessID, saleID).Scan(&line); err != nil {
		return app.SaleItem{}, err
	}
	var subtotal string
	if err := tx.QueryRow(ctx, `SELECT (($1::numeric * $2::numeric) - $3::numeric)::text`, item.Quantity, item.UnitPrice, item.Discount).Scan(&subtotal); err != nil {
		return app.SaleItem{}, err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO sale_items (business_id, sale_id, product_id, quantity, unit_price, discount, subtotal, line_number, notes)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NULLIF($9, ''))`,
		businessID, saleID, prodID, item.Quantity, item.UnitPrice, item.Discount, subtotal, line, item.Notes)
	if err != nil {
		return app.SaleItem{}, err
	}
	if err := recomputeSaleTotals(ctx, tx, saleID); err != nil {
		return app.SaleItem{}, err
	}
	if err := auditTx(ctx, tx, businessID, userID, "SALE", saleID, receiptNumber, "ITEM_ADDED", ""); err != nil {
		return app.SaleItem{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return app.SaleItem{}, err
	}
	return app.SaleItem{LineNumber: line, ProductCode: item.ProductCode, ProductName: prodName,
		Quantity: item.Quantity, UnitPrice: item.UnitPrice, Discount: item.Discount, Subtotal: subtotal, Notes: item.Notes}, nil
}

func (r *Repository) UpdateSaleItem(ctx context.Context, businessID, userID, receiptNumber string, lineNumber int, item app.NewSaleItem) (app.SaleItem, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return app.SaleItem{}, err
	}
	defer rollback(ctx, tx)

	var saleID, status string
	err = tx.QueryRow(ctx, `SELECT id, status FROM sales WHERE business_id = $1 AND receipt_number = $2 FOR UPDATE`, businessID, receiptNumber).Scan(&saleID, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.SaleItem{}, app.ErrNotFound
	}
	if err != nil {
		return app.SaleItem{}, err
	}
	if status != "DRAFT" {
		return app.SaleItem{}, &app.Error{Code: "INVALID_STATE", Message: "Item hanya dapat diubah pada sale DRAFT."}
	}
	var itemID string
	err = tx.QueryRow(ctx, `SELECT id FROM sale_items WHERE business_id = $1 AND sale_id = $2 AND line_number = $3`, businessID, saleID, lineNumber).Scan(&itemID)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.SaleItem{}, app.ErrNotFound
	}
	if err != nil {
		return app.SaleItem{}, err
	}

	productCode := item.ProductCode
	var prodID, prodName string
	if productCode == "" {
		err = tx.QueryRow(ctx, `SELECT p.public_code, p.name, p.id::text FROM products p JOIN sale_items si ON si.product_id = p.id WHERE si.id = $1`, itemID).Scan(&productCode, &prodName, &prodID)
		if err != nil {
			return app.SaleItem{}, err
		}
	} else {
		err = tx.QueryRow(ctx, `SELECT id, name FROM products WHERE business_id = $1 AND public_code = $2`, businessID, productCode).Scan(&prodID, &prodName)
		if errors.Is(err, pgx.ErrNoRows) {
			return app.SaleItem{}, app.ErrNotFound
		}
		if err != nil {
			return app.SaleItem{}, err
		}
	}
	var subtotal string
	if err := tx.QueryRow(ctx, `SELECT (($1::numeric * $2::numeric) - $3::numeric)::text`, item.Quantity, item.UnitPrice, item.Discount).Scan(&subtotal); err != nil {
		return app.SaleItem{}, err
	}
	_, err = tx.Exec(ctx, `
		UPDATE sale_items SET product_id = $1, quantity = $2, unit_price = $3, discount = $4, subtotal = $5, notes = NULLIF($6, '')
		WHERE id = $7`, prodID, item.Quantity, item.UnitPrice, item.Discount, subtotal, item.Notes, itemID)
	if err != nil {
		return app.SaleItem{}, err
	}
	if err := recomputeSaleTotals(ctx, tx, saleID); err != nil {
		return app.SaleItem{}, err
	}
	if err := auditTx(ctx, tx, businessID, userID, "SALE", saleID, receiptNumber, "ITEM_UPDATED", ""); err != nil {
		return app.SaleItem{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return app.SaleItem{}, err
	}
	return app.SaleItem{LineNumber: lineNumber, ProductCode: productCode, ProductName: prodName,
		Quantity: item.Quantity, UnitPrice: item.UnitPrice, Discount: item.Discount, Subtotal: subtotal, Notes: item.Notes}, nil
}

func (r *Repository) DeleteSaleItem(ctx context.Context, businessID, userID, receiptNumber string, lineNumber int) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer rollback(ctx, tx)

	var saleID, status string
	err = tx.QueryRow(ctx, `SELECT id, status FROM sales WHERE business_id = $1 AND receipt_number = $2 FOR UPDATE`, businessID, receiptNumber).Scan(&saleID, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.ErrNotFound
	}
	if err != nil {
		return err
	}
	if status != "DRAFT" {
		return &app.Error{Code: "INVALID_STATE", Message: "Item hanya dapat dihapus pada sale DRAFT."}
	}
	tag, err := tx.Exec(ctx, `DELETE FROM sale_items WHERE business_id = $1 AND sale_id = $2 AND line_number = $3`, businessID, saleID, lineNumber)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return app.ErrNotFound
	}
	if err := recomputeSaleTotals(ctx, tx, saleID); err != nil {
		return err
	}
	if err := auditTx(ctx, tx, businessID, userID, "SALE", saleID, receiptNumber, "ITEM_DELETED", ""); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Repository) CancelSale(ctx context.Context, businessID, userID, receiptNumber string) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer rollback(ctx, tx)

	var saleID, status string
	err = tx.QueryRow(ctx, `SELECT id, status FROM sales WHERE business_id = $1 AND receipt_number = $2 FOR UPDATE`, businessID, receiptNumber).Scan(&saleID, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.ErrNotFound
	}
	if err != nil {
		return err
	}
	if status != "DRAFT" {
		return &app.Error{Code: "INVALID_STATE", Message: "Hanya sale DRAFT yang dapat dibatalkan."}
	}
	var movements, payments int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM stock_movements WHERE business_id = $1 AND sale_id = $2`, businessID, saleID).Scan(&movements); err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM payments WHERE business_id = $1 AND sale_id = $2`, businessID, saleID).Scan(&payments); err != nil {
		return err
	}
	if movements > 0 || payments > 0 {
		return &app.Error{Code: "INVALID_STATE", Message: "Sale dengan movement/payment tidak dapat dibatalkan."}
	}
	if _, err := tx.Exec(ctx, `UPDATE sales SET status = 'CANCELLED', updated_at = now() WHERE id = $1`, saleID); err != nil {
		return err
	}
	if err := auditTx(ctx, tx, businessID, userID, "SALE", saleID, receiptNumber, "CANCELLED", ""); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Repository) CheckoutSale(ctx context.Context, businessID, userID, receiptNumber string, paymentInput app.PaymentInput) (app.Sale, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return app.Sale{}, err
	}
	defer rollback(ctx, tx)

	var saleID, locationID, status string
	var grandTotal string
	err = tx.QueryRow(ctx, `SELECT id, location_id, status, grand_total::text FROM sales WHERE business_id = $1 AND receipt_number = $2 FOR UPDATE`, businessID, receiptNumber).Scan(&saleID, &locationID, &status, &grandTotal)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.Sale{}, app.ErrNotFound
	}
	if err != nil {
		return app.Sale{}, err
	}

	if status != "DRAFT" {
		return app.Sale{}, &app.Error{Code: "INVALID_STATE", Message: "Hanya pesanan DRAFT yang dapat dicheckout"}
	}

	var lines int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM sale_items WHERE sale_id = $1`, saleID).Scan(&lines); err != nil {
		return app.Sale{}, err
	}
	if lines == 0 {
		return app.Sale{}, &app.Error{Code: "INVALID_STATE", Message: "Sale tanpa item tidak dapat dicheckout."}
	}

	var cashID string
	err = tx.QueryRow(ctx, `SELECT id FROM cash_accounts WHERE business_id = $1 AND public_code = $2 AND status = 'ACTIVE'`, businessID, paymentInput.CashAccountCode).Scan(&cashID)
	if err != nil {
		return app.Sale{}, errors.New("akun kas tidak valid")
	}

	var amountMatch bool
	err = tx.QueryRow(ctx, `SELECT $1::numeric = $2::numeric`, paymentInput.Amount, grandTotal).Scan(&amountMatch)
	if err != nil || !amountMatch {
		return app.Sale{}, &app.Error{Code: "INVALID_STATE", Message: "jumlah pembayaran harus sama dengan total tagihan"}
	}

	method := paymentInput.PaymentMethod
	if method == "" {
		method = "CASH"
	}
	paidAt := time.Now()
	if paymentInput.PaidAt != "" {
		if parsed, perr := time.Parse(time.RFC3339, paymentInput.PaidAt); perr == nil {
			paidAt = parsed
		}
	}

	rows, err := tx.Query(ctx, `
		SELECT si.product_id, si.quantity::text, p.base_unit_id, p.is_stock_tracked
		FROM sale_items si
		JOIN products p ON p.id = si.product_id
		WHERE si.sale_id = $1
		ORDER BY si.product_id`, saleID)
	if err != nil {
		return app.Sale{}, err
	}

	type itemData struct {
		prodID     string
		quantity   string
		baseUnitID string
		isTracked  bool
	}
	var items []itemData
	for rows.Next() {
		var i itemData
		if err := rows.Scan(&i.prodID, &i.quantity, &i.baseUnitID, &i.isTracked); err != nil {
			rows.Close()
			return app.Sale{}, err
		}
		items = append(items, i)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return app.Sale{}, err
	}

	for _, item := range items {
		if !item.isTracked {
			continue
		}

		result, err := tx.Exec(ctx, `
			UPDATE product_inventory
			SET quantity = quantity - $4::numeric, updated_at = now()
			WHERE business_id = $1 AND product_id = $2 AND location_id = $3 AND quantity >= $4::numeric`,
			businessID, item.prodID, locationID, item.quantity,
		)
		if err != nil {
			return app.Sale{}, err
		}
		if result.RowsAffected() == 0 {
			return app.Sale{}, &app.Error{Code: "INSUFFICIENT_STOCK", Message: "stok tidak mencukupi atau lokasi tidak ditemukan untuk produk tertentu"}
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO stock_movements (business_id, product_id, location_id, movement_type, direction, quantity, unit_id, base_quantity, base_unit_id, sale_id, created_by)
			VALUES ($1, $2, $3, 'SALE', 'OUT', $4, $5, $4, $5, $6, $7)`,
			businessID, item.prodID, locationID, item.quantity, item.baseUnitID, saleID, userID,
		)
		if err != nil {
			return app.Sale{}, err
		}
	}

	payNumber, _ := nextNumber(ctx, tx, businessID, "PAY")
	if payNumber == "" {
		payNumber = fmt.Sprintf("PAY-%s-%d", receiptNumber, time.Now().UnixMilli())
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO payments (business_id, cash_account_id, sale_id, payment_number, payment_date, payment_method, amount, kind, reference_number, notes, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'CHARGE', NULLIF($8, ''), NULLIF($9, ''), $10)`,
		businessID, cashID, saleID, payNumber, paidAt, method, paymentInput.Amount, paymentInput.ReferenceNumber, paymentInput.Notes, userID)
	if err != nil {
		return app.Sale{}, err
	}

	if _, err := tx.Exec(ctx, `UPDATE cash_accounts SET balance = balance + $1::numeric, updated_at = now() WHERE id = $2`, paymentInput.Amount, cashID); err != nil {
		return app.Sale{}, err
	}

	if _, err := tx.Exec(ctx, `UPDATE sales SET status = 'COMPLETED', payment_status = 'PAID', updated_at = now() WHERE id = $1`, saleID); err != nil {
		return app.Sale{}, err
	}
	if err := auditTx(ctx, tx, businessID, userID, "SALE", saleID, receiptNumber, "CHECKOUT", ""); err != nil {
		return app.Sale{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return app.Sale{}, err
	}

	return r.GetSale(ctx, businessID, receiptNumber)
}

func (r *Repository) VoidSale(ctx context.Context, businessID, userID, receiptNumber, reason string) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer rollback(ctx, tx)

	var saleID, locationID, status, paymentStatus string
	err = tx.QueryRow(ctx, `SELECT id, location_id, status, payment_status FROM sales WHERE business_id = $1 AND receipt_number = $2 FOR UPDATE`, businessID, receiptNumber).Scan(&saleID, &locationID, &status, &paymentStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.ErrNotFound
	}
	if err != nil {
		return err
	}

	if status != "COMPLETED" || paymentStatus != "PAID" {
		return &app.Error{Code: "INVALID_STATE", Message: "Hanya pesanan COMPLETED dan PAID yang dapat divoid"}
	}

	rows, err := tx.Query(ctx, `
		SELECT si.product_id, si.quantity::text, p.base_unit_id, p.is_stock_tracked
		FROM sale_items si
		JOIN products p ON p.id = si.product_id
		WHERE si.sale_id = $1
		ORDER BY si.product_id`, saleID)
	if err != nil {
		return err
	}
	type itemData struct {
		prodID     string
		quantity   string
		baseUnitID string
		isTracked  bool
	}
	var items []itemData
	for rows.Next() {
		var i itemData
		if err := rows.Scan(&i.prodID, &i.quantity, &i.baseUnitID, &i.isTracked); err != nil {
			rows.Close()
			return err
		}
		items = append(items, i)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, item := range items {
		if !item.isTracked {
			continue
		}
		if _, err := tx.Exec(ctx, `
			UPDATE product_inventory
			SET quantity = quantity + $4::numeric, updated_at = now()
			WHERE business_id = $1 AND product_id = $2 AND location_id = $3`,
			businessID, item.prodID, locationID, item.quantity,
		); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO stock_movements (business_id, product_id, location_id, movement_type, direction, quantity, unit_id, base_quantity, base_unit_id, sale_id, reason, created_by)
			VALUES ($1, $2, $3, 'SALE_VOID', 'IN', $4, $5, $4, $5, $6, $7, $8)`,
			businessID, item.prodID, locationID, item.quantity, item.baseUnitID, saleID, "VOID: "+reason, userID,
		); err != nil {
			return err
		}
	}

	// Append-only refund: new REFUND payment rows per original CHARGE payment.
	// Ledger history is never edited or deleted.
	payRows, err := tx.Query(ctx, `
		SELECT cash_account_id, amount::text FROM payments
		WHERE business_id = $1 AND sale_id = $2 AND kind = 'CHARGE'`, businessID, saleID)
	if err != nil {
		return err
	}
	type charge struct {
		cashID string
		amount string
	}
	var charges []charge
	for payRows.Next() {
		var c charge
		if err := payRows.Scan(&c.cashID, &c.amount); err != nil {
			payRows.Close()
			return err
		}
		charges = append(charges, c)
	}
	payRows.Close()
	if err := payRows.Err(); err != nil {
		return err
	}
	for _, c := range charges {
		refundNumber, _ := nextNumber(ctx, tx, businessID, "PAY")
		if refundNumber == "" {
			refundNumber = fmt.Sprintf("RFD-%s-%d", receiptNumber, time.Now().UnixMilli())
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO payments (business_id, cash_account_id, sale_id, payment_number, payment_method, amount, kind, notes, created_by)
			VALUES ($1, $2, $3, $4, 'CASH', $5, 'REFUND', $6, $7)`,
			businessID, c.cashID, saleID, refundNumber, c.amount, "Refund void "+receiptNumber+": "+reason, userID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE cash_accounts SET balance = balance - $1::numeric, updated_at = now() WHERE id = $2`, c.amount, c.cashID); err != nil {
			return err
		}
	}

	if _, err := tx.Exec(ctx, `UPDATE sales SET status = 'VOIDED', payment_status = 'UNPAID', notes = COALESCE(notes, '') || ' [VOID: ' || $2 || ']' WHERE id = $1`, saleID, reason); err != nil {
		return err
	}
	if err := auditTx(ctx, tx, businessID, userID, "SALE", saleID, receiptNumber, "VOID", reason); err != nil {
		return err
	}

	return tx.Commit(ctx)
}
