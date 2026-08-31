package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"usahainaja/backend/internal/app"
)

func (r *Repository) ListPurchases(ctx context.Context, businessID string) ([]app.Purchase, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT p.purchase_number, COALESCE(p.reference_number, ''), p.purchase_date, l.public_code, c.public_code, p.status, p.payment_status,
			   p.subtotal, p.discount_total, p.tax_total, p.grand_total, COALESCE(p.notes, '')
		FROM purchases p
		JOIN locations l ON l.id = p.location_id
		LEFT JOIN contacts c ON c.id = p.supplier_id
		WHERE p.business_id = $1
		ORDER BY p.purchase_date DESC LIMIT 100`, businessID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]app.Purchase, 0)
	for rows.Next() {
		var item app.Purchase
		if err := rows.Scan(&item.PurchaseNumber, &item.ReferenceNumber, &item.PurchaseDate, &item.LocationCode, &item.SupplierCode,
			&item.Status, &item.PaymentStatus, &item.Subtotal, &item.DiscountTotal, &item.TaxTotal,
			&item.GrandTotal, &item.Notes); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
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

	var supplierID *string
	if input.SupplierCode != "" {
		var cid string
		err = tx.QueryRow(ctx, `SELECT id FROM contacts WHERE business_id = $1 AND public_code = $2`, businessID, input.SupplierCode).Scan(&cid)
		if err == nil {
			supplierID = &cid
		}
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
		) VALUES ($1, $2, $3, $4, NULLIF($5, ''), 'DRAFT', $6, $7, $8, $9) RETURNING id`,
		businessID, locationID, supplierID, purchaseNumber, input.ReferenceNumber, input.PaymentStatus, input.DiscountTotal, input.TaxTotal, userID,
	).Scan(&purchaseID)
	if err != nil {
		return app.Purchase{}, err
	}

	for _, item := range input.Items {
		var prodID string
		err = tx.QueryRow(ctx, `SELECT id FROM products WHERE business_id = $1 AND public_code = $2`, businessID, item.ProductCode).Scan(&prodID)
		if errors.Is(err, pgx.ErrNoRows) {
			return app.Purchase{}, app.ErrNotFound
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO purchase_items (business_id, purchase_id, product_id, quantity, unit_price, discount, subtotal)
			VALUES ($1, $2, $3, $4, $5, $6, ($4::numeric * $5::numeric) - $6::numeric)`,
			businessID, purchaseID, prodID, item.Quantity, item.UnitPrice, item.Discount,
		)
		if err != nil {
			return app.Purchase{}, err
		}
	}

	// Update totals
	_, err = tx.Exec(ctx, `
		UPDATE purchases 
		SET subtotal = (SELECT COALESCE(SUM(subtotal), 0) FROM purchase_items WHERE purchase_id = $1),
			grand_total = (SELECT COALESCE(SUM(subtotal), 0) FROM purchase_items WHERE purchase_id = $1) - discount_total + tax_total
		WHERE id = $1`, purchaseID)
	if err != nil {
		return app.Purchase{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return app.Purchase{}, err
	}
	
	var purchase app.Purchase
	purchase.PurchaseNumber = purchaseNumber
	return purchase, nil
}

func (r *Repository) ReceivePurchase(ctx context.Context, businessID, purchaseNumber, userID string) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer rollback(ctx, tx)

	var purchaseID, locationID string
	err = tx.QueryRow(ctx, `
		UPDATE purchases SET status = 'COMPLETED', updated_at = now()
		WHERE business_id = $1 AND purchase_number = $2 AND status = 'DRAFT'
		RETURNING id, location_id`, businessID, purchaseNumber).Scan(&purchaseID, &locationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.ErrNotFound
	}
	if err != nil {
		return err
	}

	rows, err := tx.Query(ctx, `
		SELECT pi.product_id, pi.quantity, p.base_unit_id, p.is_stock_tracked
		FROM purchase_items pi
		JOIN products p ON p.id = pi.product_id
		WHERE pi.business_id = $1 AND pi.purchase_id = $2`, businessID, purchaseID)
	if err != nil {
		return err
	}
	defer rows.Close()

	type item struct {
		prodID    string
		quantity  string
		baseUnit  string
		isTracked bool
	}
	var items []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.prodID, &it.quantity, &it.baseUnit, &it.isTracked); err != nil {
			return err
		}
		items = append(items, it)
	}
	rows.Close()

	for _, it := range items {
		if !it.isTracked {
			continue
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO stock_movements (business_id, product_id, location_id, movement_type, direction, quantity, unit_id, base_quantity, base_unit_id, reference_id, created_by)
			VALUES ($1, $2, $3, 'PURCHASE', 'IN', $4, $5, $4, $5, $6, $7)`,
			businessID, it.prodID, locationID, it.quantity, it.baseUnit, purchaseID, userID)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO product_inventory (business_id, product_id, location_id, quantity, base_unit_id)
			VALUES ($1, $2, $3, $4::numeric, $5)
			ON CONFLICT (business_id, product_id, location_id) DO UPDATE SET quantity = product_inventory.quantity + $4::numeric`,
			businessID, it.prodID, locationID, it.quantity, it.baseUnit)
		if err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

func (r *Repository) RecordPurchasePayment(ctx context.Context, businessID, purchaseNumber, userID string, in app.PaymentInput) (app.Payment, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return app.Payment{}, err
	}
	defer rollback(ctx, tx)

	var cashID string
	err = tx.QueryRow(ctx, `SELECT id FROM cash_accounts WHERE business_id = $1 AND public_code = $2 AND status = 'ACTIVE'`, businessID, in.CashAccountCode).Scan(&cashID)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.Payment{}, app.ErrNotFound
	}

	var purchaseID string
	err = tx.QueryRow(ctx, `SELECT id FROM purchases WHERE business_id = $1 AND purchase_number = $2`, businessID, purchaseNumber).Scan(&purchaseID)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.Payment{}, app.ErrNotFound
	}

	payNumber, err := nextNumber(ctx, tx, businessID, "PAY")
	if err != nil || payNumber == "" {
		payNumber = fmt.Sprintf("PAY-%d", time.Now().UnixMilli())
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO payments (business_id, cash_account_id, purchase_id, payment_number, amount, reference_number, notes, created_by)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), NULLIF($7, ''), $8)`,
		businessID, cashID, purchaseID, payNumber, in.Amount, in.ReferenceNumber, in.Notes, userID)
	if err != nil {
		return app.Payment{}, err
	}

	_, err = tx.Exec(ctx, `UPDATE cash_accounts SET balance = balance - $1::numeric WHERE id = $2`, in.Amount, cashID)
	if err != nil {
		return app.Payment{}, err
	}

	var paidTotal, grandTotal float64
	err = tx.QueryRow(ctx, `
		SELECT COALESCE((SELECT SUM(amount) FROM payments WHERE purchase_id = p.id), 0), p.grand_total
		FROM purchases p WHERE p.id = $1`, purchaseID).Scan(&paidTotal, &grandTotal)
	if err != nil {
		return app.Payment{}, err
	}

	newStatus := "PARTIAL"
	if paidTotal >= grandTotal {
		newStatus = "PAID"
	}

	_, err = tx.Exec(ctx, `UPDATE purchases SET payment_status = $1, updated_at = now() WHERE id = $2`, newStatus, purchaseID)
	if err != nil {
		return app.Payment{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return app.Payment{}, err
	}

	return app.Payment{
		PaymentNumber:   payNumber,
		CashAccountCode: in.CashAccountCode,
		PaymentDate:     time.Now(),
		Amount:          in.Amount,
		ReferenceNumber: in.ReferenceNumber,
		Notes:           in.Notes,
	}, nil
}
