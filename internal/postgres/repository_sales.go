package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"usahainaja/backend/internal/app"
)

func (r *Repository) ListSales(ctx context.Context, businessID string) ([]app.Sale, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT s.receipt_number, s.sale_date, l.public_code, c.public_code, s.status, s.payment_status,
			   s.subtotal, s.discount_total, s.tax_total, s.grand_total, COALESCE(s.notes, '')
		FROM sales s
		JOIN locations l ON l.id = s.location_id
		LEFT JOIN contacts c ON c.id = s.customer_id
		WHERE s.business_id = $1
		ORDER BY s.sale_date DESC LIMIT 100`, businessID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]app.Sale, 0)
	for rows.Next() {
		var item app.Sale
		if err := rows.Scan(&item.ReceiptNumber, &item.SaleDate, &item.LocationCode, &item.CustomerCode,
			&item.Status, &item.PaymentStatus, &item.Subtotal, &item.DiscountTotal, &item.TaxTotal,
			&item.GrandTotal, &item.Notes); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
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

	// Calculate totals inside postgres by just inserting items, but we need subtotal first.
	// For simplicity in this demo logic, we'll let postgres trigger or calculate it, 
	// OR we compute it in Go. Let's compute in Go for precise string decimal insertion.
	// We will skip strict decimal math here and just let Postgres do `SUM`.

	var saleID string
	err = tx.QueryRow(ctx, `
		INSERT INTO sales (
			business_id, location_id, customer_id, receipt_number, status, payment_status, 
			discount_total, tax_total, created_by
		) VALUES ($1, $2, $3, $4, 'DRAFT', 'UNPAID', $5, $6, $7) RETURNING id`,
		businessID, locationID, customerID, receiptNumber, input.DiscountTotal, input.TaxTotal, userID,
	).Scan(&saleID)
	if err != nil {
		return app.Sale{}, err
	}

	for _, item := range input.Items {
		var prodID string
		err = tx.QueryRow(ctx, `SELECT id FROM products WHERE business_id = $1 AND public_code = $2`, businessID, item.ProductCode).Scan(&prodID)
		if errors.Is(err, pgx.ErrNoRows) {
			return app.Sale{}, app.ErrNotFound
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO sale_items (business_id, sale_id, product_id, quantity, unit_price, discount, subtotal)
			VALUES ($1, $2, $3, $4, $5, $6, ($4::numeric * $5::numeric) - $6::numeric)`,
			businessID, saleID, prodID, item.Quantity, item.UnitPrice, item.Discount,
		)
		if err != nil {
			return app.Sale{}, err
		}
	}

	// Update totals
	_, err = tx.Exec(ctx, `
		UPDATE sales 
		SET subtotal = (SELECT COALESCE(SUM(subtotal), 0) FROM sale_items WHERE sale_id = $1),
			grand_total = (SELECT COALESCE(SUM(subtotal), 0) FROM sale_items WHERE sale_id = $1) - discount_total + tax_total
		WHERE id = $1`, saleID)
	if err != nil {
		return app.Sale{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return app.Sale{}, err
	}

	_, _ = r.pool.Exec(ctx, `
		INSERT INTO audit_logs (business_id, actor_user_id, entity_type, entity_id, entity_code, action, after_data)
		VALUES ($1, $2, 'SALE', $3, $4, 'CREATE', '{}')`,
		businessID, userID, saleID, receiptNumber,
	)
	
	// Fetch result
	var sale app.Sale
	sale.ReceiptNumber = receiptNumber
	return sale, nil
}

func (r *Repository) CheckoutSale(ctx context.Context, businessID, userID, receiptNumber string, paymentInput app.PaymentInput) (app.Sale, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return app.Sale{}, err
	}
	defer rollback(ctx, tx)

	var saleID, locationID, status string
	var grandTotal string
	err = tx.QueryRow(ctx, `SELECT id, location_id, status, grand_total FROM sales WHERE business_id = $1 AND receipt_number = $2 FOR UPDATE`, businessID, receiptNumber).Scan(&saleID, &locationID, &status, &grandTotal)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.Sale{}, app.ErrNotFound
	}
	if err != nil {
		return app.Sale{}, err
	}

	if status != "DRAFT" {
		return app.Sale{}, &app.Error{Code: "INVALID_STATE", Message: "Hanya pesanan DRAFT yang dapat dicheckout"}
	}

	var cashID string
	err = tx.QueryRow(ctx, `SELECT id FROM cash_accounts WHERE business_id = $1 AND public_code = $2`, businessID, paymentInput.CashAccountCode).Scan(&cashID)
	if err != nil {
		return app.Sale{}, errors.New("akun kas tidak valid")
	}

	// verify amount
	var amountMatch bool
	err = tx.QueryRow(ctx, `SELECT $1::numeric = $2::numeric`, paymentInput.Amount, grandTotal).Scan(&amountMatch)
	if err != nil || !amountMatch {
		return app.Sale{}, &app.Error{Code: "INVALID_STATE", Message: "jumlah pembayaran harus sama dengan total tagihan"}
	}

	// Update stock for all tracked items
	rows, err := tx.Query(ctx, `
		SELECT si.product_id, si.quantity, p.base_unit_id, p.is_stock_tracked 
		FROM sale_items si 
		JOIN products p ON p.id = si.product_id
		WHERE si.sale_id = $1
		ORDER BY si.product_id -- consistent locking order`, saleID)
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
			return app.Sale{}, err
		}
		items = append(items, i)
	}
	rows.Close()

	for _, item := range items {
		if !item.isTracked {
			continue
		}

		// check stock and lock
		result, err := tx.Exec(ctx, `
			UPDATE product_inventory 
			SET quantity = quantity - $4::numeric
			WHERE business_id = $1 AND product_id = $2 AND location_id = $3 AND quantity >= $4::numeric`,
			businessID, item.prodID, locationID, item.quantity,
		)
		if err != nil {
			return app.Sale{}, err
		}
		if result.RowsAffected() == 0 {
			return app.Sale{}, &app.Error{Code: "INSUFFICIENT_STOCK", Message: "stok tidak mencukupi atau lokasi tidak ditemukan untuk produk tertentu"}
		}

		// movement OUT
		_, err = tx.Exec(ctx, `
			INSERT INTO stock_movements (business_id, product_id, location_id, movement_type, direction, quantity, unit_id, base_quantity, base_unit_id, reference_id, created_by)
			VALUES ($1, $2, $3, 'SALE', 'OUT', $4, $5, $4, $5, $6, $7)`,
			businessID, item.prodID, locationID, item.quantity, item.baseUnitID, saleID, userID,
		)
		if err != nil {
			return app.Sale{}, err
		}
	}

	// Create payment
	payNumber, _ := nextNumber(ctx, tx, businessID, "PAY")
	if payNumber == "" {
		payNumber = fmt.Sprintf("PAY-%s-%d", receiptNumber, time.Now().UnixMilli())
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO payments (business_id, cash_account_id, sale_id, payment_number, amount, created_by)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		businessID, cashID, saleID, payNumber, paymentInput.Amount, userID)
	if err != nil {
		return app.Sale{}, err
	}
	
	_, err = tx.Exec(ctx, `UPDATE cash_accounts SET balance = balance + $1::numeric WHERE id = $2`, paymentInput.Amount, cashID)
	if err != nil {
		return app.Sale{}, err
	}

	// Complete sale
	_, err = tx.Exec(ctx, `UPDATE sales SET status = 'COMPLETED', payment_status = 'PAID' WHERE id = $1`, saleID)
	if err != nil {
		return app.Sale{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return app.Sale{}, err
	}

	_, _ = r.pool.Exec(ctx, `
		INSERT INTO audit_logs (business_id, actor_user_id, entity_type, entity_id, entity_code, action, after_data)
		VALUES ($1, $2, 'SALE', $3, $4, 'CHECKOUT', '{}')`,
		businessID, userID, saleID, receiptNumber,
	)

	var sale app.Sale
	sale.ReceiptNumber = receiptNumber
	sale.Status = "COMPLETED"
	sale.PaymentStatus = "PAID"
	return sale, nil
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

	// Return stock
	rows, err := tx.Query(ctx, `
		SELECT si.product_id, si.quantity, p.base_unit_id, p.is_stock_tracked 
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
			return err
		}
		items = append(items, i)
	}
	rows.Close()

	for _, item := range items {
		if !item.isTracked {
			continue
		}
		_, err = tx.Exec(ctx, `
			UPDATE product_inventory 
			SET quantity = quantity + $4::numeric
			WHERE business_id = $1 AND product_id = $2 AND location_id = $3`,
			businessID, item.prodID, locationID, item.quantity,
		)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO stock_movements (business_id, product_id, location_id, movement_type, direction, quantity, unit_id, base_quantity, base_unit_id, reference_id, reason, created_by)
			VALUES ($1, $2, $3, 'SALE', 'IN', $4, $5, $4, $5, $6, $7, $8)`,
			businessID, item.prodID, locationID, item.quantity, item.baseUnitID, saleID, "VOID: "+reason, userID,
		)
		if err != nil {
			return err
		}
	}

	// Reverse payments
	var cashID, amount string
	err = tx.QueryRow(ctx, `SELECT cash_account_id, amount FROM payments WHERE sale_id = $1 LIMIT 1`, saleID).Scan(&cashID, &amount)
	if err == nil {
		_, err = tx.Exec(ctx, `UPDATE cash_accounts SET balance = balance - $1::numeric WHERE id = $2`, amount, cashID)
		if err != nil {
			return err
		}
		// Delete the payment record so it doesn't violate amount > 0 constraint if we tried to insert negative
		_, err = tx.Exec(ctx, `DELETE FROM payments WHERE sale_id = $1`, saleID)
		if err != nil {
			return err
		}
	}

	_, err = tx.Exec(ctx, `UPDATE sales SET status = 'REFUNDED', payment_status = 'REFUNDED', notes = COALESCE(notes, '') || ' [VOID: ' || $2 || ']' WHERE id = $1`, saleID, reason)
	if err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	_, _ = r.pool.Exec(ctx, `
		INSERT INTO audit_logs (business_id, actor_user_id, entity_type, entity_id, entity_code, action, reason, after_data)
		VALUES ($1, $2, 'SALE', $3, $4, 'VOID', $5, '{}')`,
		businessID, userID, saleID, receiptNumber, reason,
	)
	
	return nil
}
