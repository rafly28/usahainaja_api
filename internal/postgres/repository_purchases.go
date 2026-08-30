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
		) VALUES ($1, $2, $3, $4, NULLIF($5, ''), 'COMPLETED', $6, $7, $8, $9) RETURNING id`,
		businessID, locationID, supplierID, purchaseNumber, input.ReferenceNumber, input.PaymentStatus, input.DiscountTotal, input.TaxTotal, userID,
	).Scan(&purchaseID)
	if err != nil {
		return app.Purchase{}, err
	}

	for _, item := range input.Items {
		var prodID, baseUnitID string
		var isTracked bool
		err = tx.QueryRow(ctx, `SELECT id, base_unit_id, is_stock_tracked FROM products WHERE business_id = $1 AND public_code = $2`, businessID, item.ProductCode).Scan(&prodID, &baseUnitID, &isTracked)
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

		if isTracked {
			_, err = tx.Exec(ctx, `
				INSERT INTO stock_movements (business_id, product_id, location_id, movement_type, direction, quantity, unit_id, base_quantity, base_unit_id, reference_id, created_by)
				VALUES ($1, $2, $3, 'PURCHASE', 'IN', $4, $5, $4, $5, $6, $7)`,
				businessID, prodID, locationID, item.Quantity, baseUnitID, purchaseID, userID,
			)
			if err != nil {
				return app.Purchase{}, err
			}
			_, err = tx.Exec(ctx, `
				INSERT INTO product_inventory (business_id, product_id, location_id, quantity, base_unit_id)
				VALUES ($1, $2, $3, $4::numeric, $5)
				ON CONFLICT (business_id, product_id, location_id) DO UPDATE SET quantity = product_inventory.quantity + $4::numeric`,
				businessID, prodID, locationID, item.Quantity, baseUnitID,
			)
			if err != nil {
				return app.Purchase{}, err
			}
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

	// If paid, create payment
	if input.PaymentStatus == "PAID" {
		var cashID string
		err = tx.QueryRow(ctx, `SELECT id FROM cash_accounts WHERE business_id = $1 AND is_default = true LIMIT 1`, businessID).Scan(&cashID)
		if err == nil {
			payNumber, _ := nextNumber(ctx, tx, businessID, "PAY")
			if payNumber == "" { payNumber = "PAY-" + purchaseNumber }
			_, err = tx.Exec(ctx, `
				INSERT INTO payments (business_id, cash_account_id, purchase_id, payment_number, amount, created_by)
				SELECT $1, $2, $3, $4, grand_total, $5 FROM purchases WHERE id = $3`,
				businessID, cashID, purchaseID, payNumber, userID)
			if err != nil {
				return app.Purchase{}, err
			}
			_, err = tx.Exec(ctx, `UPDATE cash_accounts SET balance = balance - (SELECT grand_total FROM purchases WHERE id = $1) WHERE id = $2`, purchaseID, cashID)
			if err != nil {
				return app.Purchase{}, err
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return app.Purchase{}, err
	}
	
	var purchase app.Purchase
	purchase.PurchaseNumber = purchaseNumber
	return purchase, nil
}
