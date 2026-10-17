package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"usahainaja/backend/internal/app"
)

// inventoryFromClause dipakai bersama oleh query jumlah dan query isi laporan stok
// supaya total dan daftar selalu memakai definisi yang sama.
const inventoryFromClause = `
	FROM products p
	JOIN units u ON u.id = p.base_unit_id AND u.business_id = p.business_id
	CROSS JOIN locations l
	LEFT JOIN product_inventory pi
	  ON pi.business_id = p.business_id AND pi.product_id = p.id AND pi.location_id = l.id
`

// stockUnitResolution menyimpan hasil resolusi unit untuk satu baris stok.
type stockUnitResolution struct {
	ProductID      string
	ProductName    string
	BaseUnitID     string
	BaseUnitSymbol string
	UnitID         string
	UnitSymbol     string
	Multiplier     string
}

// locationIDTx mencari lokasi aktif milik bisnis atau mengembalikan app.ErrNotFound.
func locationIDTx(ctx context.Context, tx pgx.Tx, businessID, locationCode string) (string, error) {
	var locationID string
	err := tx.QueryRow(ctx, `
		SELECT id::text FROM locations
		WHERE business_id = $1 AND public_code = $2 AND status = 'ACTIVE'`,
		businessID, strings.ToUpper(strings.TrimSpace(locationCode))).Scan(&locationID)
	if err != nil {
		return "", mapError(err)
	}
	return locationID, nil
}

// resolveStockUnitTx menentukan unit yang dipakai pengguna, unit dasar produk, dan
// pengali konversi ke unit dasar. Unit dasar selalu memakai pengali 1; unit lain
// wajib punya unit_conversions yang terdefinisi agar movement dan snapshot konsisten.
func resolveStockUnitTx(ctx context.Context, tx pgx.Tx, businessID, productCode, unitSymbol string) (stockUnitResolution, error) {
	var out stockUnitResolution
	err := tx.QueryRow(ctx, `
		SELECT p.id::text, p.name, p.base_unit_id::text, bu.symbol
		FROM products p
		JOIN units bu ON bu.id = p.base_unit_id AND bu.business_id = p.business_id
		WHERE p.business_id = $1 AND p.public_code = $2
		  AND p.status = 'ACTIVE' AND p.is_stock_tracked = true`,
		businessID, strings.ToUpper(strings.TrimSpace(productCode)),
	).Scan(&out.ProductID, &out.ProductName, &out.BaseUnitID, &out.BaseUnitSymbol)
	if err != nil {
		return stockUnitResolution{}, mapError(err)
	}

	unitSymbol = strings.TrimSpace(unitSymbol)
	if unitSymbol == "" || strings.EqualFold(unitSymbol, out.BaseUnitSymbol) {
		out.UnitID = out.BaseUnitID
		out.UnitSymbol = out.BaseUnitSymbol
		out.Multiplier = "1"
		return out, nil
	}

	err = tx.QueryRow(ctx, `
		SELECT u.id::text, u.symbol, uc.multiplier::text
		FROM units u
		JOIN unit_conversions uc ON uc.business_id = u.business_id
			AND uc.from_unit_id = u.id AND uc.to_unit_id = $4 AND uc.product_id = $3
		WHERE u.business_id = $1 AND upper(u.symbol) = upper($2)`,
		businessID, unitSymbol, out.ProductID, out.BaseUnitID,
	).Scan(&out.UnitID, &out.UnitSymbol, &out.Multiplier)
	if err != nil {
		if strings.Contains(err.Error(), "no rows") {
			return stockUnitResolution{}, app.ErrUnitConversion
		}
		return stockUnitResolution{}, mapError(err)
	}
	return out, nil
}

// currentInventoryQtyTx mengunci baris snapshot stok dan mengembalikan kuantitas
// saat ini dalam unit dasar. Snapshot tidak ada berarti 0.
func currentInventoryQtyTx(ctx context.Context, tx pgx.Tx, businessID, productID, locationID string) (string, error) {
	var quantity string
	err := tx.QueryRow(ctx, `
		SELECT quantity::text FROM product_inventory
		WHERE business_id = $1 AND product_id = $2 AND location_id = $3
		FOR UPDATE`, businessID, productID, locationID).Scan(&quantity)
	if err != nil {
		if strings.Contains(err.Error(), "no rows") {
			return "0", nil
		}
		return "", err
	}
	return quantity, nil
}

// applyStockDeltaTx menambahkan delta (sudah dalam unit dasar) ke snapshot stok.
//
// PostgreSQL mengevaluasi CHECK constraint pada tuple yang di-insert sebelum
// ON CONFLICT DO UPDATE, sehingga delta negatif tidak boleh masuk lewat INSERT ... ON CONFLICT.
// Karena itu pola yang dipakai adalah UPDATE dulu, dan hanya INSERT bila baris belum ada.
func applyStockDeltaTx(ctx context.Context, tx pgx.Tx, businessID, productID, locationID, baseUnitID, delta string) (string, error) {
	var quantity string
	err := tx.QueryRow(ctx, `
		UPDATE product_inventory
		SET quantity = quantity + $4::numeric, updated_at = now()
		WHERE business_id = $1 AND product_id = $2 AND location_id = $3
		RETURNING quantity::text`, businessID, productID, locationID, delta).Scan(&quantity)
	if err == nil {
		return quantity, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}

	// Baris belum ada; pemanggil sudah memastikan hasil akhir tidak negatif,
	// sehingga delta di titik ini hanya boleh positif.
	err = tx.QueryRow(ctx, `
		INSERT INTO product_inventory (business_id, product_id, location_id, quantity, base_unit_id)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING quantity::text`, businessID, productID, locationID, delta, baseUnitID).Scan(&quantity)
	if err != nil {
		return "", err
	}
	return quantity, nil
}

// writeAuditTx menulis audit log beserta after_data terstruktur.
func writeAuditTx(ctx context.Context, tx pgx.Tx, businessID, userID, entityType, entityID, entityCode, action string, afterData any, reason string) error {
	payload := "{}"
	if afterData != nil {
		encoded, err := json.Marshal(afterData)
		if err != nil {
			return err
		}
		payload = string(encoded)
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO audit_logs (
			business_id, actor_user_id, entity_type, entity_id, entity_code, action, after_data, reason
		) VALUES ($1, NULLIF($2, '')::uuid, $3, NULLIF($4, '')::uuid, $5, $6, $7::jsonb, NULLIF($8, ''))`,
		businessID, userID, entityType, entityID, entityCode, action, payload, reason)
	return err
}

// movementTypeForReason memetakan alasan adjustment ke movement_type yang valid.
// Nilai alasan seperti CORRECTION/LOST/OTHER tidak ada di enum movement_type,
// sehingga memakai ADJUSTMENT agar tidak melanggar CHECK constraint.
func movementTypeForReason(reason string) string {
	switch strings.ToUpper(strings.TrimSpace(reason)) {
	case "SPOILAGE":
		return "SPOILAGE"
	case "DAMAGE":
		return "DAMAGE"
	default:
		return "ADJUSTMENT"
	}
}

// pageArgs mengembalikan limit dan offset yang aman dari page/limit mentah.
func pageArgs(page, limit, defLimit, maxLimit int) (int, int) {
	if limit <= 0 {
		limit = defLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	if page <= 0 {
		page = 1
	}
	return limit, (page - 1) * limit
}

// CreateOpeningStock mencatat stok awal batch untuk satu lokasi dalam satu transaksi.
func (r *Repository) CreateOpeningStock(ctx context.Context, businessID, userID string, input app.NewOpeningStock) (app.OpeningStock, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return app.OpeningStock{}, err
	}
	defer rollback(ctx, tx)

	locationID, err := locationIDTx(ctx, tx, businessID, input.LocationCode)
	if err != nil {
		return app.OpeningStock{}, err
	}

	number, err := nextNumber(ctx, tx, businessID, "OPENING_STOCK")
	if err != nil {
		return app.OpeningStock{}, err
	}

	var adjustmentID string
	err = tx.QueryRow(ctx, `
		INSERT INTO stock_adjustments (
			business_id, location_id, adjustment_number, reason, status, notes, created_by, adjustment_kind
		) VALUES ($1, $2, $3, 'OPENING_BALANCE', 'COMPLETED', NULLIF($4, ''), $5, 'OPENING_BALANCE')
		RETURNING id::text`, businessID, locationID, number, input.Reason, userID).Scan(&adjustmentID)
	if err != nil {
		return app.OpeningStock{}, mapError(err)
	}

	items := make([]app.OpeningStockItem, 0, len(input.Items))
	for _, item := range input.Items {
		resolved, err := resolveStockUnitTx(ctx, tx, businessID, item.ProductCode, item.UnitSymbol)
		if err != nil {
			return app.OpeningStock{}, err
		}
		baseQuantity, err := multiplyDecimal(item.Quantity, resolved.Multiplier, stockScale)
		if err != nil {
			return app.OpeningStock{}, err
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO stock_adjustment_items (
				business_id, stock_adjustment_id, product_id, quantity, unit_id,
				base_quantity, base_unit_id, direction
			) VALUES ($1, $2, $3, $4, $5, $6, $7, 'IN')`,
			businessID, adjustmentID, resolved.ProductID, item.Quantity,
			resolved.UnitID, baseQuantity, resolved.BaseUnitID); err != nil {
			return app.OpeningStock{}, mapError(err)
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO stock_movements (
				business_id, product_id, location_id, movement_type, direction,
				quantity, unit_id, base_quantity, base_unit_id, reference_id, reason, created_by
			) VALUES ($1, $2, $3, 'OPENING_BALANCE', 'IN', $4, $5, $6, $7, $8, NULLIF($9, ''), $10)`,
			businessID, resolved.ProductID, locationID, item.Quantity, resolved.UnitID,
			baseQuantity, resolved.BaseUnitID, adjustmentID, input.Reason, userID); err != nil {
			return app.OpeningStock{}, mapError(err)
		}

		current, err := applyStockDeltaTx(ctx, tx, businessID, resolved.ProductID, locationID, resolved.BaseUnitID, baseQuantity)
		if err != nil {
			return app.OpeningStock{}, err
		}

		items = append(items, app.OpeningStockItem{
			ProductCode:     strings.ToUpper(strings.TrimSpace(item.ProductCode)),
			ProductName:     resolved.ProductName,
			Quantity:        item.Quantity,
			UnitSymbol:      resolved.UnitSymbol,
			CurrentQuantity: current,
		})
	}

	if err := writeAuditTx(ctx, tx, businessID, userID, "STOCK_ADJUSTMENT", adjustmentID, number,
		"OPENING_STOCK_RECORDED",
		map[string]any{"location_code": strings.ToUpper(input.LocationCode), "items": items},
		input.Reason); err != nil {
		return app.OpeningStock{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return app.OpeningStock{}, err
	}
	return app.OpeningStock{
		AdjustmentNumber: number,
		LocationCode:     strings.ToUpper(strings.TrimSpace(input.LocationCode)),
		Items:            items,
	}, nil
}

// ListInventoryProducts membaca stok per produk dan lokasi dengan filter kontrak.
func (r *Repository) ListInventoryProducts(ctx context.Context, businessID string, filter app.InventoryFilter) ([]app.InventoryProduct, int, error) {
	where := `WHERE p.business_id = $1 AND p.status = 'ACTIVE' AND p.is_stock_tracked = true
		AND l.business_id = $1 AND l.status = 'ACTIVE'`
	args := []any{businessID}

	if filter.LocationCode != "" {
		args = append(args, strings.ToUpper(filter.LocationCode))
		where += fmt.Sprintf(" AND l.public_code = $%d", len(args))
	}
	if filter.Search != "" {
		args = append(args, "%"+filter.Search+"%")
		where += fmt.Sprintf(" AND (p.name ILIKE $%[1]d OR p.sku ILIKE $%[1]d OR p.public_code ILIKE $%[1]d)", len(args))
	}
	if filter.LowStockOnly {
		where += ` AND COALESCE(pi.quantity, 0) <= p.min_stock`
	}

	var total int
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*)`+inventoryFromClause+where, args...).Scan(&total); err != nil {
		return nil, 0, mapError(err)
	}

	limit, offset := pageArgs(filter.Page, filter.Limit, 200, 500)
	args = append(args, limit, offset)
	query := `
		SELECT p.public_code, p.name, COALESCE(p.sku, ''), u.symbol,
		       COALESCE(pi.quantity, 0)::text, p.min_stock::text,
		       l.public_code, l.name, (COALESCE(pi.quantity, 0) <= p.min_stock)` +
		inventoryFromClause + where +
		fmt.Sprintf(" ORDER BY p.name, l.name LIMIT $%d OFFSET $%d", len(args)-1, len(args))

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, mapError(err)
	}
	defer rows.Close()

	items := make([]app.InventoryProduct, 0)
	for rows.Next() {
		var item app.InventoryProduct
		if err := rows.Scan(&item.ProductCode, &item.Name, &item.SKU, &item.UnitSymbol,
			&item.Quantity, &item.MinStock, &item.LocationCode, &item.LocationName,
			&item.IsLowStock); err != nil {
			return nil, 0, mapError(err)
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}
