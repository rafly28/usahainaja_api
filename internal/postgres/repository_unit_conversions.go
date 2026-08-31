package postgres

import (
	"context"
	"usahainaja/backend/internal/app"
)

func (r *Repository) ListUnitConversions(ctx context.Context, businessID string) ([]app.UnitConversion, error) {
	rows, err := r.pool.Query(ctx, `SELECT COALESCE(p.public_code,''),fu.public_code,tu.public_code,uc.multiplier::text FROM unit_conversions uc JOIN units fu ON fu.id=uc.from_unit_id JOIN units tu ON tu.id=uc.to_unit_id LEFT JOIN products p ON p.id=uc.product_id WHERE uc.business_id=$1 ORDER BY p.public_code NULLS FIRST,fu.public_code,tu.public_code`, businessID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]app.UnitConversion, 0)
	for rows.Next() {
		var item app.UnitConversion
		if err := rows.Scan(&item.ProductCode, &item.FromUnitCode, &item.ToUnitCode, &item.Multiplier); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) CreateUnitConversion(ctx context.Context, businessID, userID string, input app.NewUnitConversion) (app.UnitConversion, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return app.UnitConversion{}, err
	}
	defer rollback(ctx, tx)
	var fromID, toID, productID string
	if err = tx.QueryRow(ctx, `SELECT id::text FROM units WHERE business_id=$1 AND public_code=$2 AND status='ACTIVE'`, businessID, input.FromUnitCode).Scan(&fromID); err != nil {
		return app.UnitConversion{}, mapError(err)
	}
	if err = tx.QueryRow(ctx, `SELECT id::text FROM units WHERE business_id=$1 AND public_code=$2 AND status='ACTIVE'`, businessID, input.ToUnitCode).Scan(&toID); err != nil {
		return app.UnitConversion{}, mapError(err)
	}
	if input.ProductCode != "" {
		if err = tx.QueryRow(ctx, `SELECT id::text FROM products WHERE business_id=$1 AND public_code=$2 AND status='ACTIVE'`, businessID, input.ProductCode).Scan(&productID); err != nil {
			return app.UnitConversion{}, mapError(err)
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO unit_conversions (business_id,product_id,from_unit_id,to_unit_id,multiplier) VALUES ($1,NULLIF($2,'')::uuid,$3,$4,$5)`, businessID, productID, fromID, toID, input.Multiplier)
	if err != nil {
		return app.UnitConversion{}, mapError(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_logs (business_id,actor_user_id,entity_type,entity_code,action,after_data) VALUES ($1,$2,'UNIT_CONVERSION',$3,'UNIT_CONVERSION_CREATED',jsonb_build_object('from_unit_code',$4::text,'to_unit_code',$5::text,'multiplier',$6::text))`, businessID, userID, input.ProductCode, input.FromUnitCode, input.ToUnitCode, input.Multiplier); err != nil {
		return app.UnitConversion{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return app.UnitConversion{}, err
	}
	return app.UnitConversion{ProductCode: input.ProductCode, FromUnitCode: input.FromUnitCode, ToUnitCode: input.ToUnitCode, Multiplier: input.Multiplier}, nil
}
