package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"usahainaja/backend/internal/app"
)

// ListStockMovements membaca ledger pergerakan stok dengan filter dan pagination kontrak.
// month range dipakai agar batas tanggal_to inklusif mengikuti zona waktu bisnis.
func (r *Repository) ListStockMovements(ctx context.Context, businessID string, filter app.StockMovementFilter) ([]app.StockMovement, int, error) {
	where := `WHERE sm.business_id = $1`
	args := []any{businessID}

	if filter.ProductCode != "" {
		args = append(args, strings.ToUpper(filter.ProductCode))
		where += fmt.Sprintf(" AND p.public_code = $%d", len(args))
	}
	if filter.LocationCode != "" {
		args = append(args, strings.ToUpper(filter.LocationCode))
		where += fmt.Sprintf(" AND l.public_code = $%d", len(args))
	}
	if filter.MovementType != "" {
		args = append(args, filter.MovementType)
		where += fmt.Sprintf(" AND sm.movement_type = $%d", len(args))
	}
	if filter.DateFrom != "" {
		args = append(args, filter.DateFrom)
		where += fmt.Sprintf(" AND sm.occurred_at >= $%d::date", len(args))
	}
	if filter.DateTo != "" {
		args = append(args, filter.DateTo)
		where += fmt.Sprintf(" AND sm.occurred_at < ($%d::date + interval '1 day')", len(args))
	}

	base := `
		FROM stock_movements sm
		JOIN products p ON p.id = sm.product_id
		JOIN locations l ON l.id = sm.location_id
		JOIN units u ON sm.unit_id = u.id
		LEFT JOIN users us ON us.id = sm.created_by
	`

	var total int
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*)`+base+where, args...).Scan(&total); err != nil {
		return nil, 0, mapError(err)
	}

	limit, offset := pageArgs(filter.Page, filter.Limit, 50, 200)
	args = append(args, limit, offset)
	query := `
		SELECT sm.movement_type, sm.direction, sm.quantity::text, u.symbol,
		       p.public_code, p.name, l.public_code, l.name,
		       COALESCE(sm.reason, ''), sm.occurred_at, COALESCE(us.name, '')` +
		base + where +
		fmt.Sprintf(" ORDER BY sm.occurred_at DESC, sm.id DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args))

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, mapError(err)
	}
	defer rows.Close()

	movements := make([]app.StockMovement, 0)
	for rows.Next() {
		var m app.StockMovement
		if err := rows.Scan(&m.MovementType, &m.Direction, &m.Quantity, &m.UnitSymbol,
			&m.ProductCode, &m.ProductName, &m.LocationCode, &m.LocationName,
			&m.Reason, &m.OccurredAt, &m.CreatedByName); err != nil {
			return nil, 0, mapError(err)
		}
		movements = append(movements, m)
	}
	return movements, total, rows.Err()
}

// ListStockAdjustments membaca daftar header adjustment (draft dan completed).
func (r *Repository) ListStockAdjustments(ctx context.Context, businessID string, filter app.StockAdjustmentFilter) ([]app.StockAdjustment, int, error) {
	where := "\n\t\tWHERE sa.business_id = $1"
	args := []any{businessID}
	if filter.Status != "" {
		args = append(args, filter.Status)
		where += fmt.Sprintf(" AND sa.status = $%d", len(args))
	}
	if filter.Kind != "" {
		args = append(args, filter.Kind)
		where += fmt.Sprintf(" AND sa.adjustment_kind = $%d", len(args))
	}

	var total int
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM stock_adjustments sa`+where, args...).Scan(&total); err != nil {
		return nil, 0, mapError(err)
	}

	limit, offset := pageArgs(filter.Page, filter.Limit, 50, 200)
	args = append(args, limit, offset)
	rows, err := r.pool.Query(ctx, `
		SELECT sa.adjustment_number, l.public_code, sa.reason, sa.adjustment_kind,
		       sa.status, COALESCE(sa.notes, ''), sa.adjustment_date, COALESCE(us.name, '')
		FROM stock_adjustments sa
		JOIN locations l ON l.id = sa.location_id
		LEFT JOIN users us ON us.id = sa.created_by`+where+
		fmt.Sprintf(" ORDER BY sa.adjustment_date DESC, sa.adjustment_number DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args)),
		args...)
	if err != nil {
		return nil, 0, mapError(err)
	}
	defer rows.Close()

	adjustments := make([]app.StockAdjustment, 0)
	for rows.Next() {
		var a app.StockAdjustment
		if err := rows.Scan(&a.AdjustmentNumber, &a.LocationCode, &a.Reason, &a.Kind,
			&a.Status, &a.Notes, &a.AdjustmentDate, &a.CreatedByName); err != nil {
			return nil, 0, mapError(err)
		}
		adjustments = append(adjustments, a)
	}
	return adjustments, total, rows.Err()
}

// GetStockAdjustment membaca satu adjustment beserta seluruh barisnya.
func (r *Repository) GetStockAdjustment(ctx context.Context, businessID, adjustmentNumber string) (app.StockAdjustment, error) {
	var a app.StockAdjustment
	var adjustmentID string
	err := r.pool.QueryRow(ctx, `
		SELECT sa.id::text, sa.adjustment_number, l.public_code, sa.reason, sa.adjustment_kind,
		       sa.status, COALESCE(sa.notes, ''), sa.adjustment_date, COALESCE(us.name, '')
		FROM stock_adjustments sa
		JOIN locations l ON l.id = sa.location_id
		LEFT JOIN users us ON us.id = sa.created_by
		WHERE sa.business_id = $1 AND sa.adjustment_number = $2`,
		businessID, adjustmentNumber,
	).Scan(&adjustmentID, &a.AdjustmentNumber, &a.LocationCode, &a.Reason, &a.Kind,
		&a.Status, &a.Notes, &a.AdjustmentDate, &a.CreatedByName)
	if err != nil {
		return app.StockAdjustment{}, mapError(err)
	}

	items, err := r.adjustmentItems(ctx, businessID, adjustmentID)
	if err != nil {
		return app.StockAdjustment{}, err
	}
	a.Items = items
	return a, nil
}

// adjustmentItems membaca baris adjustment beserta jejak opname bila tersedia.
func (r *Repository) adjustmentItems(ctx context.Context, businessID, adjustmentID string) ([]app.StockAdjustmentItem, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT p.public_code, p.name, sai.quantity::text, u.symbol, sai.direction,
		       COALESCE(sai.system_quantity::text, ''), COALESCE(sai.counted_quantity::text, '')
		FROM stock_adjustment_items sai
		JOIN products p ON p.id = sai.product_id
		JOIN units u ON u.id = sai.unit_id
		WHERE sai.business_id = $1 AND sai.stock_adjustment_id = $2
		ORDER BY sai.id`, businessID, adjustmentID)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()

	items := make([]app.StockAdjustmentItem, 0)
	for rows.Next() {
		var it app.StockAdjustmentItem
		if err := rows.Scan(&it.ProductCode, &it.ProductName, &it.Quantity, &it.UnitSymbol,
			&it.Direction, &it.SystemQuantity, &it.CountedQuantity); err != nil {
			return nil, mapError(err)
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

// CreateStockAdjustment membuat adjustment manual berstatus DRAFT.
// Semua efek stok baru terjadi saat CompleteStockAdjustment dipanggil.
func (r *Repository) CreateStockAdjustment(ctx context.Context, businessID, userID string, input app.NewStockAdjustment) (app.StockAdjustment, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return app.StockAdjustment{}, err
	}
	defer rollback(ctx, tx)

	locationID, err := locationIDTx(ctx, tx, businessID, input.LocationCode)
	if err != nil {
		return app.StockAdjustment{}, err
	}
	number, err := nextNumber(ctx, tx, businessID, "STOCK_ADJUSTMENT")
	if err != nil {
		return app.StockAdjustment{}, err
	}

	adjustmentID, kind, err := insertAdjustmentHeader(ctx, tx, businessID, userID, locationID, number, "MANUAL", input.Reason, input.Notes)
	if err != nil {
		return app.StockAdjustment{}, err
	}

	items := make([]app.StockAdjustmentItem, 0, len(input.Items))
	for _, item := range input.Items {
		resolved, err := resolveStockUnitTx(ctx, tx, businessID, item.ProductCode, item.UnitSymbol)
		if err != nil {
			return app.StockAdjustment{}, err
		}
		baseQuantity, err := multiplyDecimal(item.Quantity, resolved.Multiplier, stockScale)
		if err != nil {
			return app.StockAdjustment{}, err
		}
		if err := insertAdjustmentItem(ctx, tx, businessID, adjustmentID, resolved, item.Quantity, baseQuantity, item.Direction, "", ""); err != nil {
			return app.StockAdjustment{}, err
		}
		items = append(items, app.StockAdjustmentItem{
			ProductCode: strings.ToUpper(strings.TrimSpace(item.ProductCode)),
			ProductName: resolved.ProductName,
			Quantity:    item.Quantity,
			UnitSymbol:  resolved.UnitSymbol,
			Direction:   item.Direction,
		})
	}

	if err := writeAuditTx(ctx, tx, businessID, userID, "STOCK_ADJUSTMENT", adjustmentID, number,
		"STOCK_ADJUSTMENT_DRAFT_CREATED",
		map[string]any{"location_code": strings.ToUpper(input.LocationCode), "items": items},
		input.Reason); err != nil {
		return app.StockAdjustment{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return app.StockAdjustment{}, err
	}
	return app.StockAdjustment{
		AdjustmentNumber: number,
		LocationCode:     strings.ToUpper(strings.TrimSpace(input.LocationCode)),
		Reason:           input.Reason,
		Kind:             kind,
		Status:           "DRAFT",
		Notes:            input.Notes,
		AdjustmentDate:   time.Now(),
		Items:            items,
	}, nil
}

// insertAdjustmentHeader membuat header adjustment seragam untuk manual maupun opname.
func insertAdjustmentHeader(ctx context.Context, tx pgx.Tx, businessID, userID, locationID, number, kind, reason, notes string) (string, string, error) {
	var adjustmentID string
	err := tx.QueryRow(ctx, `
		INSERT INTO stock_adjustments (
			business_id, location_id, adjustment_number, reason, status, notes, created_by, adjustment_kind
		) VALUES ($1, $2, $3, $4, 'DRAFT', NULLIF($5, ''), $6, $7)
		RETURNING id::text`, businessID, locationID, number, reason, notes, userID, kind).Scan(&adjustmentID)
	if err != nil {
		return "", "", mapError(err)
	}
	return adjustmentID, kind, nil
}

// insertAdjustmentItem menulis satu baris adjustment lengkap dengan basis konversi.
func insertAdjustmentItem(ctx context.Context, tx pgx.Tx, businessID, adjustmentID string, resolved stockUnitResolution, quantity, baseQuantity, direction, systemQuantity, countedQuantity string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO stock_adjustment_items (
			business_id, stock_adjustment_id, product_id, quantity, unit_id,
			base_quantity, base_unit_id, direction, system_quantity, counted_quantity
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8,
		          NULLIF($9, '')::numeric, NULLIF($10, '')::numeric)`,
		businessID, adjustmentID, resolved.ProductID, quantity, resolved.UnitID,
		baseQuantity, resolved.BaseUnitID, direction, systemQuantity, countedQuantity)
	if err != nil {
		return mapError(err)
	}
	return nil
}

// CreateStockOpname membuat adjustment OPNAME dari hasil hitung fisik.
// Kuantitas sistem dibaca di dalam transaksi, bukan dipercaya dari payload.
func (r *Repository) CreateStockOpname(ctx context.Context, businessID, userID string, input app.NewStockOpname) (app.StockAdjustment, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return app.StockAdjustment{}, err
	}
	defer rollback(ctx, tx)

	locationID, err := locationIDTx(ctx, tx, businessID, input.LocationCode)
	if err != nil {
		return app.StockAdjustment{}, err
	}
	number, err := nextNumber(ctx, tx, businessID, "STOCK_OPNAME")
	if err != nil {
		return app.StockAdjustment{}, err
	}

	adjustmentID, kind, err := insertAdjustmentHeader(ctx, tx, businessID, userID, locationID, number, "OPNAME", input.Reason, input.Notes)
	if err != nil {
		return app.StockAdjustment{}, err
	}

	items := make([]app.StockAdjustmentItem, 0, len(input.Items))
	countedLines := make([]map[string]string, 0, len(input.Items))
	for _, item := range input.Items {
		resolved, err := resolveStockUnitTx(ctx, tx, businessID, item.ProductCode, item.UnitSymbol)
		if err != nil {
			return app.StockAdjustment{}, err
		}
		counted, err := multiplyDecimal(item.CountedQuantity, resolved.Multiplier, stockScale)
		if err != nil {
			return app.StockAdjustment{}, err
		}

		systemQuantity, err := currentInventoryQtyTx(ctx, tx, businessID, resolved.ProductID, locationID)
		if err != nil {
			return app.StockAdjustment{}, err
		}

		difference, err := differenceDecimal(counted, systemQuantity, stockScale)
		if err != nil {
			return app.StockAdjustment{}, err
		}

		productCode := strings.ToUpper(strings.TrimSpace(item.ProductCode))
		direction := "NO_CHANGE"
		if !isZeroDecimal(difference) {
			comparison, err := compareDecimal(counted, systemQuantity)
			if err != nil {
				return app.StockAdjustment{}, err
			}
			direction = "IN"
			if comparison < 0 {
				direction = "OUT"
			}

			baseQuantity, err := multiplyDecimal(difference, resolved.Multiplier, stockScale)
			if err != nil {
				return app.StockAdjustment{}, err
			}
			if err := insertAdjustmentItem(ctx, tx, businessID, adjustmentID, resolved, difference, baseQuantity, direction, systemQuantity, counted); err != nil {
				return app.StockAdjustment{}, err
			}
			items = append(items, app.StockAdjustmentItem{
				ProductCode:     productCode,
				ProductName:     resolved.ProductName,
				Quantity:        difference,
				UnitSymbol:      resolved.UnitSymbol,
				Direction:       direction,
				SystemQuantity:  systemQuantity,
				CountedQuantity: counted,
			})
		}

		// Jejak hitung fisik tetap dicatat walau tidak ada selisih.
		countedLines = append(countedLines, map[string]string{
			"product_code":     productCode,
			"product_name":     resolved.ProductName,
			"system_quantity":  systemQuantity,
			"counted_quantity": counted,
			"difference":       difference,
			"unit_symbol":      resolved.UnitSymbol,
			"direction":        direction,
		})
	}

	if len(items) == 0 {
		return app.StockAdjustment{}, app.ErrNoStockDifference
	}

	if err := writeAuditTx(ctx, tx, businessID, userID, "STOCK_ADJUSTMENT", adjustmentID, number,
		"STOCK_OPNAME_RECORDED",
		map[string]any{"location_code": strings.ToUpper(input.LocationCode), "counted_lines": countedLines},
		input.Reason); err != nil {
		return app.StockAdjustment{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return app.StockAdjustment{}, err
	}
	return app.StockAdjustment{
		AdjustmentNumber: number,
		LocationCode:     strings.ToUpper(strings.TrimSpace(input.LocationCode)),
		Reason:           input.Reason,
		Kind:             kind,
		Status:           "DRAFT",
		Notes:            input.Notes,
		AdjustmentDate:   time.Now(),
		Items:            items,
	}, nil
}
