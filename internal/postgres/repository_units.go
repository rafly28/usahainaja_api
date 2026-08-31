package postgres

import (
	"context"

	"usahainaja/backend/internal/app"
)

func (r *Repository) ListUnits(ctx context.Context, businessID string) ([]app.Unit, error) {
	rows, err := r.pool.Query(ctx, `SELECT public_code,name,symbol,unit_type,status FROM units WHERE business_id=$1 ORDER BY name,public_code`, businessID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]app.Unit, 0)
	for rows.Next() {
		var item app.Unit
		if err := rows.Scan(&item.Code, &item.Name, &item.Symbol, &item.UnitType, &item.Status); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) CreateUnit(ctx context.Context, businessID, userID string, input app.NewUnit) (app.Unit, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return app.Unit{}, err
	}
	defer rollback(ctx, tx)
	code, err := nextNumber(ctx, tx, businessID, "UNIT")
	if err != nil {
		return app.Unit{}, err
	}
	var unitID string
	err = tx.QueryRow(ctx, `INSERT INTO units (business_id,public_code,name,symbol,unit_type) VALUES ($1,$2,$3,$4,$5) RETURNING id::text`, businessID, code, input.Name, input.Symbol, input.UnitType).Scan(&unitID)
	if err != nil {
		return app.Unit{}, mapError(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_logs (business_id,actor_user_id,entity_type,entity_id,entity_code,action,after_data) VALUES ($1,$2,'UNIT',$3,$4,'UNIT_CREATED',jsonb_build_object('name',$5::text,'symbol',$6::text))`, businessID, userID, unitID, code, input.Name, input.Symbol); err != nil {
		return app.Unit{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return app.Unit{}, err
	}
	return app.Unit{Code: code, Name: input.Name, Symbol: input.Symbol, UnitType: input.UnitType, Status: "ACTIVE"}, nil
}

func (r *Repository) UpdateUnit(ctx context.Context, businessID, userID, code, status string, input app.NewUnit) (app.Unit, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return app.Unit{}, err
	}
	defer rollback(ctx, tx)
	var unitID string
	err = tx.QueryRow(ctx, `UPDATE units SET name=$3,symbol=$4,unit_type=$5,status=$6,updated_at=now() WHERE business_id=$1 AND public_code=$2 RETURNING id::text`, businessID, code, input.Name, input.Symbol, input.UnitType, status).Scan(&unitID)
	if err != nil {
		return app.Unit{}, mapError(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_logs (business_id,actor_user_id,entity_type,entity_id,entity_code,action,after_data) VALUES ($1,$2,'UNIT',$3,$4,'UNIT_UPDATED',jsonb_build_object('name',$5::text,'symbol',$6::text,'status',$7::text))`, businessID, userID, unitID, code, input.Name, input.Symbol, status); err != nil {
		return app.Unit{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return app.Unit{}, err
	}
	return app.Unit{Code: code, Name: input.Name, Symbol: input.Symbol, UnitType: input.UnitType, Status: status}, nil
}
