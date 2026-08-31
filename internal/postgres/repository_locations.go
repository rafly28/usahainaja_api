package postgres

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"usahainaja/backend/internal/app"
)

func (r *Repository) ListLocations(ctx context.Context, businessID string) ([]app.Location, error) {
	rows, err := r.pool.Query(ctx, `SELECT public_code,name,type,COALESCE(address,''),is_default,status FROM locations WHERE business_id=$1 ORDER BY is_default DESC,name,public_code`, businessID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]app.Location, 0)
	for rows.Next() {
		var item app.Location
		if err := rows.Scan(&item.Code, &item.Name, &item.Type, &item.Address, &item.IsDefault, &item.Status); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
func (r *Repository) CreateLocation(ctx context.Context, businessID, userID string, input app.NewLocation) (app.Location, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return app.Location{}, err
	}
	defer rollback(ctx, tx)
	var activeCount int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM locations WHERE business_id=$1 AND status='ACTIVE'`, businessID).Scan(&activeCount); err != nil {
		return app.Location{}, err
	}
	if activeCount == 0 {
		input.IsDefault = true
	}
	if input.IsDefault {
		if _, err = tx.Exec(ctx, `UPDATE locations SET is_default=false WHERE business_id=$1`, businessID); err != nil {
			return app.Location{}, err
		}
	}
	code, err := nextNumber(ctx, tx, businessID, "LOCATION")
	if err != nil {
		return app.Location{}, err
	}
	var id string
	err = tx.QueryRow(ctx, `INSERT INTO locations (business_id,public_code,name,type,address,is_default) VALUES ($1,$2,$3,$4,NULLIF($5,''),$6) RETURNING id::text`, businessID, code, input.Name, input.Type, input.Address, input.IsDefault).Scan(&id)
	if err != nil {
		return app.Location{}, mapError(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_logs (business_id,actor_user_id,entity_type,entity_id,entity_code,action,after_data) VALUES ($1,$2,'LOCATION',$3,$4,'LOCATION_CREATED',jsonb_build_object('name',$5::text,'type',$6::text,'is_default',$7::boolean))`, businessID, userID, id, code, input.Name, input.Type, input.IsDefault); err != nil {
		return app.Location{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return app.Location{}, err
	}
	return app.Location{Code: code, Name: input.Name, Type: input.Type, Address: input.Address, IsDefault: input.IsDefault, Status: "ACTIVE"}, nil
}
func (r *Repository) UpdateLocation(ctx context.Context, businessID, userID, code, status string, input app.NewLocation) (app.Location, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return app.Location{}, err
	}
	defer rollback(ctx, tx)
	var id string
	var oldDefault bool
	err = tx.QueryRow(ctx, `SELECT id::text,is_default FROM locations WHERE business_id=$1 AND public_code=$2 FOR UPDATE`, businessID, code).Scan(&id, &oldDefault)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.Location{}, app.ErrNotFound
	}
	if err != nil {
		return app.Location{}, err
	}
	if oldDefault && !input.IsDefault {
		return app.Location{}, app.ErrConflict
	}
	if oldDefault && status != "ACTIVE" {
		return app.Location{}, app.ErrNotFound
	}
	if input.IsDefault {
		if _, err = tx.Exec(ctx, `UPDATE locations SET is_default=false WHERE business_id=$1 AND public_code<>$2`, businessID, code); err != nil {
			return app.Location{}, err
		}
	}
	err = tx.QueryRow(ctx, `UPDATE locations SET name=$3,type=$4,address=NULLIF($5,''),is_default=$6,status=$7,updated_at=now() WHERE business_id=$1 AND public_code=$2 RETURNING id::text`, businessID, code, input.Name, input.Type, input.Address, input.IsDefault, status).Scan(&id)
	if err != nil {
		return app.Location{}, mapError(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_logs (business_id,actor_user_id,entity_type,entity_id,entity_code,action,after_data) VALUES ($1,$2,'LOCATION',$3,$4,'LOCATION_UPDATED',jsonb_build_object('name',$5::text,'status',$6::text,'is_default',$7::boolean))`, businessID, userID, id, code, input.Name, status, input.IsDefault); err != nil {
		return app.Location{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return app.Location{}, err
	}
	return app.Location{Code: code, Name: input.Name, Type: input.Type, Address: input.Address, IsDefault: input.IsDefault, Status: status}, nil
}
