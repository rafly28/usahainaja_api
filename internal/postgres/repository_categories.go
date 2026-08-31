package postgres

import (
	"context"

	"usahainaja/backend/internal/app"
)

func (r *Repository) ListCategories(ctx context.Context, businessID, categoryType string) ([]app.Category, error) {
	query := `SELECT c.public_code, c.name, c.category_type, parent.public_code, c.status
		FROM categories c LEFT JOIN categories parent ON parent.id = c.parent_id
		WHERE c.business_id = $1`
	args := []any{businessID}
	if categoryType != "" {
		query += ` AND c.category_type = $2`
		args = append(args, categoryType)
	}
	query += ` ORDER BY c.category_type, c.name, c.public_code`
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]app.Category, 0)
	for rows.Next() {
		var item app.Category
		if err := rows.Scan(&item.Code, &item.Name, &item.CategoryType, &item.ParentCode, &item.Status); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) CreateCategory(ctx context.Context, businessID, userID string, input app.NewCategory) (app.Category, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return app.Category{}, err
	}
	defer rollback(ctx, tx)
	code, err := nextNumber(ctx, tx, businessID, "CATEGORY")
	if err != nil {
		return app.Category{}, err
	}
	var parentID *string
	if input.ParentCode != "" {
		var value string
		err = tx.QueryRow(ctx, `SELECT id::text FROM categories WHERE business_id=$1 AND public_code=$2 AND category_type=$3`, businessID, input.ParentCode, input.CategoryType).Scan(&value)
		if err != nil {
			return app.Category{}, mapError(err)
		}
		parentID = &value
	}
	item := app.Category{Code: code, Name: input.Name, CategoryType: input.CategoryType, Status: "ACTIVE"}
	if input.ParentCode != "" {
		item.ParentCode = &input.ParentCode
	}
	var categoryID string
	err = tx.QueryRow(ctx, `INSERT INTO categories (business_id,parent_id,public_code,category_type,name) VALUES ($1,$2,$3,$4,$5) RETURNING id::text`, businessID, parentID, code, input.CategoryType, input.Name).Scan(&categoryID)
	if err != nil {
		return app.Category{}, mapError(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_logs (business_id,actor_user_id,entity_type,entity_id,entity_code,action,after_data) VALUES ($1,$2,'CATEGORY',$3,$4,'CATEGORY_CREATED',jsonb_build_object('name',$5::text,'category_type',$6::text))`, businessID, userID, categoryID, code, input.Name, input.CategoryType); err != nil {
		return app.Category{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return app.Category{}, err
	}
	return item, nil
}

func (r *Repository) UpdateCategory(ctx context.Context, businessID, userID, code, status string, input app.NewCategory) (app.Category, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return app.Category{}, err
	}
	defer rollback(ctx, tx)
	var parentID *string
	if input.ParentCode != "" {
		var value string
		err = tx.QueryRow(ctx, `SELECT id::text FROM categories WHERE business_id=$1 AND public_code=$2 AND category_type=$3 AND public_code<>$4`, businessID, input.ParentCode, input.CategoryType, code).Scan(&value)
		if err != nil {
			return app.Category{}, mapError(err)
		}
		parentID = &value
	}
	item := app.Category{Code: code, Name: input.Name, CategoryType: input.CategoryType, Status: status}
	if input.ParentCode != "" {
		item.ParentCode = &input.ParentCode
	}
	var categoryID string
	err = tx.QueryRow(ctx, `UPDATE categories SET parent_id=$3,category_type=$4,name=$5,status=$6,updated_at=now() WHERE business_id=$1 AND public_code=$2 RETURNING id::text`, businessID, code, parentID, input.CategoryType, input.Name, status).Scan(&categoryID)
	if err != nil {
		return app.Category{}, mapError(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_logs (business_id,actor_user_id,entity_type,entity_id,entity_code,action,after_data) VALUES ($1,$2,'CATEGORY',$3,$4,'CATEGORY_UPDATED',jsonb_build_object('name',$5::text,'category_type',$6::text,'status',$7::text))`, businessID, userID, categoryID, code, input.Name, input.CategoryType, status); err != nil {
		return app.Category{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return app.Category{}, err
	}
	return item, nil
}
