package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"usahainaja/backend/internal/app"
)

type Repository struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (r *Repository) Ping(ctx context.Context) error { return r.pool.Ping(ctx) }

func (r *Repository) CreateUserAndSession(ctx context.Context, user app.NewUser, session app.NewSession, previousHash []byte) (app.UserRecord, app.Session, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return app.UserRecord{}, app.Session{}, err
	}
	defer rollback(ctx, tx)
	if len(previousHash) != 0 {
		if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, previousHash); err != nil {
			return app.UserRecord{}, app.Session{}, err
		}
	}
	record := app.UserRecord{User: app.User{Code: user.Code, Name: user.Name, Email: user.Email}}
	err = tx.QueryRow(ctx, `
		INSERT INTO users (public_code, name, email, password_hash)
		VALUES ($1, $2, $3, $4)
		RETURNING id::text`, user.Code, user.Name, user.Email, user.PasswordHash,
	).Scan(&record.ID)
	if err != nil {
		return app.UserRecord{}, app.Session{}, mapError(err)
	}
	created, err := insertSession(ctx, tx, record, session, nil)
	if err != nil {
		return app.UserRecord{}, app.Session{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return app.UserRecord{}, app.Session{}, err
	}
	return record, created, nil
}

func (r *Repository) FindUserByEmail(ctx context.Context, email string) (app.UserRecord, error) {
	var record app.UserRecord
	err := r.pool.QueryRow(ctx, `
		SELECT id::text, public_code, name, email, password_hash
		FROM users
		WHERE lower(email) = lower($1) AND status = 'ACTIVE'`, email,
	).Scan(&record.ID, &record.User.Code, &record.User.Name, &record.User.Email, &record.PasswordHash)
	if err != nil {
		return app.UserRecord{}, mapError(err)
	}
	return record, nil
}

func (r *Repository) ReplaceSession(ctx context.Context, userID string, session app.NewSession, previousHash []byte) (app.Session, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return app.Session{}, err
	}
	defer rollback(ctx, tx)
	if len(previousHash) != 0 {
		if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, previousHash); err != nil {
			return app.Session{}, err
		}
	}
	var record app.UserRecord
	err = tx.QueryRow(ctx, `
		SELECT id::text, public_code, name, email, password_hash
		FROM users WHERE id = $1 AND status = 'ACTIVE'`, userID,
	).Scan(&record.ID, &record.User.Code, &record.User.Name, &record.User.Email, &record.PasswordHash)
	if err != nil {
		return app.Session{}, mapError(err)
	}
	var activeID *string
	err = tx.QueryRow(ctx, `
		SELECT b.id::text
		FROM business_members bm
		JOIN businesses b ON b.id = bm.business_id AND b.status = 'ACTIVE'
		WHERE bm.user_id = $1 AND bm.status = 'ACTIVE'
		ORDER BY bm.joined_at NULLS LAST, bm.created_at
		LIMIT 1`, userID,
	).Scan(&activeID)
	if errors.Is(err, pgx.ErrNoRows) {
		activeID = nil
	} else if err != nil {
		return app.Session{}, err
	}
	created, err := insertSession(ctx, tx, record, session, activeID)
	if err != nil {
		return app.Session{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return app.Session{}, err
	}
	return created, nil
}

func insertSession(ctx context.Context, tx pgx.Tx, user app.UserRecord, input app.NewSession, activeID *string) (app.Session, error) {
	created := app.Session{
		UserID: user.ID, User: user.User, ActiveBusinessID: activeID,
		CSRFToken: input.CSRFToken, ExpiresAt: input.ExpiresAt,
	}
	err := tx.QueryRow(ctx, `
		INSERT INTO sessions (token_hash, csrf_token, user_id, active_business_id, expires_at, user_agent, ip_address)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), NULLIF($7, '')::inet)
		RETURNING id::text`, input.TokenHash, input.CSRFToken, user.ID, activeID,
		input.ExpiresAt, input.Meta.UserAgent, input.Meta.IPAddress,
	).Scan(&created.ID)
	return created, err
}

func (r *Repository) LoadSession(ctx context.Context, tokenHash []byte) (app.Session, error) {
	var session app.Session
	err := r.pool.QueryRow(ctx, `
		SELECT s.id::text, s.user_id::text, u.public_code, u.name, u.email,
		       s.active_business_id::text, s.csrf_token, s.expires_at
		FROM sessions s
		JOIN users u ON u.id = s.user_id AND u.status = 'ACTIVE'
		WHERE s.token_hash = $1 AND s.expires_at > now()`, tokenHash,
	).Scan(&session.ID, &session.UserID, &session.User.Code, &session.User.Name,
		&session.User.Email, &session.ActiveBusinessID, &session.CSRFToken, &session.ExpiresAt)
	if err != nil {
		return app.Session{}, mapError(err)
	}
	return session, nil
}

func (r *Repository) DeleteSession(ctx context.Context, sessionID string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM sessions WHERE id = $1`, sessionID)
	return err
}

func (r *Repository) ListBusinesses(ctx context.Context, userID string) ([]app.Business, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT b.public_code, b.name, b.business_type, b.timezone, b.currency, r.code,
		       l.public_code, l.name,
		       ARRAY(SELECT module_code FROM business_modules m WHERE m.business_id = b.id
		             ORDER BY array_position(ARRAY['CATALOG', 'INVENTORY', 'SALES', 'PURCHASE', 'FINANCE', 'BOOKING', 'REPORTING'], module_code))
		FROM business_members bm
		JOIN businesses b ON b.id = bm.business_id AND b.status = 'ACTIVE'
		JOIN roles r ON r.id = bm.role_id AND r.business_id = b.id
		LEFT JOIN locations l ON l.business_id = b.id AND l.is_default AND l.status = 'ACTIVE'
		WHERE bm.user_id = $1 AND bm.status = 'ACTIVE'
		ORDER BY b.name, b.public_code`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]app.Business, 0)
	for rows.Next() {
		var item app.Business
		var locationCode, locationName *string
		if err := rows.Scan(&item.Code, &item.Name, &item.BusinessType, &item.Timezone,
			&item.Currency, &item.Role, &locationCode, &locationName, &item.EnabledModules); err != nil {
			return nil, err
		}
		if locationCode != nil && locationName != nil {
			item.DefaultLocation = &app.Location{Code: *locationCode, Name: *locationName}
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) CreateBusiness(ctx context.Context, userID, sessionID string, input app.NewBusiness) (app.BusinessContext, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return app.BusinessContext{}, err
	}
	defer rollback(ctx, tx)

	var businessID, roleID, locationID string
	err = tx.QueryRow(ctx, `
		INSERT INTO businesses (public_code, name, business_type, timezone, currency)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id::text`, input.Code, input.Name, input.BusinessType, input.Timezone, input.Currency,
	).Scan(&businessID)
	if err != nil {
		return app.BusinessContext{}, mapError(err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO roles (business_id, name, code, is_system_role) VALUES
			($1, 'Owner', 'OWNER', true),
			($1, 'Admin', 'ADMIN', true),
			($1, 'Cashier', 'CASHIER', true),
			($1, 'Staff', 'STAFF', true),
			($1, 'Viewer', 'VIEWER', true)`, businessID); err != nil {
		return app.BusinessContext{}, err
	}
	err = tx.QueryRow(ctx, `
		SELECT id::text FROM roles WHERE business_id = $1 AND code = 'OWNER'`, businessID,
	).Scan(&roleID)
	if err != nil {
		return app.BusinessContext{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO business_members (business_id, user_id, role_id, status, joined_at)
		VALUES ($1, $2, $3, 'ACTIVE', now())`, businessID, userID, roleID); err != nil {
		return app.BusinessContext{}, err
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO locations (business_id, public_code, name, type, is_default)
		VALUES ($1, $2, 'Lokasi Utama', 'STORE', true) RETURNING id::text`, businessID, input.LocationCode,
	).Scan(&locationID)
	if err != nil {
		return app.BusinessContext{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO units (business_id, public_code, name, symbol, unit_type) VALUES
			($1, $2, 'Pieces', 'PCS', 'COUNT'),
			($1, 'UNT-KG', 'Kilogram', 'KG', 'WEIGHT'),
			($1, 'UNT-GRAM', 'Gram', 'GRAM', 'WEIGHT'),
			($1, 'UNT-LITER', 'Liter', 'LITER', 'VOLUME'),
			($1, 'UNT-ML', 'Milliliter', 'ML', 'VOLUME')`, businessID, input.UnitCode); err != nil {
		return app.BusinessContext{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO number_sequences (business_id, sequence_type, prefix)
		VALUES ($1, 'PRODUCT', 'PRD'), ($1, 'OPENING_STOCK', 'OPEN'),
		       ($1, 'STOCK_ADJUSTMENT', 'ADJ'), ($1, 'CONTACT', 'CNT'),
		       ($1, 'CASH', 'CSH'), ($1, 'SALE', 'SL'), ($1, 'PURC', 'PUR'),
		       ($1, 'PAY', 'PAY')`, businessID); err != nil {
		return app.BusinessContext{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO cash_accounts (business_id, public_code, name, account_type, balance, is_default)
		VALUES ($1, 'CSH-DEFAULT', 'Kas Utama', 'CASH', 0, true)`, businessID); err != nil {
		return app.BusinessContext{}, err
	}
	for _, module := range input.EnabledModules {
		if _, err := tx.Exec(ctx, `INSERT INTO business_modules (business_id, module_code) VALUES ($1, $2)`, businessID, module); err != nil {
			return app.BusinessContext{}, err
		}
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_logs (business_id, actor_user_id, entity_type, entity_id, entity_code, action, after_data)
		VALUES ($1, $2, 'BUSINESS', $1, $3, 'BUSINESS_CREATED',
		        jsonb_build_object('name', $4::text, 'business_type', $5::text, 'timezone', $6::text, 'currency', $7::text))`,
		businessID, userID, input.Code, input.Name, input.BusinessType, input.Timezone, input.Currency); err != nil {
		return app.BusinessContext{}, err
	}
	command, err := tx.Exec(ctx, `
		UPDATE sessions SET active_business_id = $1, last_seen_at = now()
		WHERE id = $2 AND user_id = $3`, businessID, sessionID, userID)
	if err != nil {
		return app.BusinessContext{}, err
	}
	if command.RowsAffected() != 1 {
		return app.BusinessContext{}, app.ErrForbidden
	}
	if err := tx.Commit(ctx); err != nil {
		return app.BusinessContext{}, err
	}
	return app.BusinessContext{
		ID: businessID, Role: "OWNER",
		Business: app.Business{
			Code: input.Code, Name: input.Name, BusinessType: input.BusinessType,
			Timezone: input.Timezone, Currency: input.Currency, Role: "OWNER",
			EnabledModules:  input.EnabledModules,
			DefaultLocation: &app.Location{Code: input.LocationCode, Name: "Lokasi Utama"},
		},
	}, nil
}

func (r *Repository) UpdateBusinessConfiguration(ctx context.Context, businessID, userID, businessType string, modules []string) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer rollback(ctx, tx)

	command, err := tx.Exec(ctx, `UPDATE businesses SET business_type = $2, updated_at = now() WHERE id = $1`, businessID, businessType)
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return app.ErrNotFound
	}
	if _, err := tx.Exec(ctx, `DELETE FROM business_modules WHERE business_id = $1`, businessID); err != nil {
		return err
	}
	for _, module := range modules {
		if _, err := tx.Exec(ctx, `INSERT INTO business_modules (business_id, module_code) VALUES ($1, $2)`, businessID, module); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_logs (business_id, actor_user_id, entity_type, entity_id, action, after_data)
		VALUES ($1, $2, 'BUSINESS', $1, 'BUSINESS_CONFIGURATION_UPDATED', jsonb_build_object('business_type', $3::text, 'enabled_modules', $4::text[]))`,
		businessID, userID, businessType, modules); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Repository) GetBusinessContext(ctx context.Context, userID, businessID string) (app.BusinessContext, error) {
	return getBusinessContext(ctx, r.pool, userID, `b.id = $2`, businessID)
}

func (r *Repository) SwitchBusiness(ctx context.Context, sessionID, userID, businessCode string) (app.BusinessContext, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return app.BusinessContext{}, err
	}
	defer rollback(ctx, tx)
	business, err := getBusinessContext(ctx, tx, userID, `b.public_code = $2`, businessCode)
	if err != nil {
		if errors.Is(err, app.ErrNotFound) {
			return app.BusinessContext{}, app.ErrForbidden
		}
		return app.BusinessContext{}, err
	}
	command, err := tx.Exec(ctx, `UPDATE sessions SET active_business_id = $1, last_seen_at = now() WHERE id = $2 AND user_id = $3`, business.ID, sessionID, userID)
	if err != nil {
		return app.BusinessContext{}, err
	}
	if command.RowsAffected() != 1 {
		return app.BusinessContext{}, app.ErrForbidden
	}
	if err := tx.Commit(ctx); err != nil {
		return app.BusinessContext{}, err
	}
	return business, nil
}

type queryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func getBusinessContext(ctx context.Context, q queryRower, userID, predicate, value string) (app.BusinessContext, error) {
	var item app.BusinessContext
	var locationCode, locationName *string
	query := `
		SELECT b.id::text, b.public_code, b.name, b.business_type, b.timezone, b.currency, r.code,
		       l.public_code, l.name,
		       ARRAY(SELECT module_code FROM business_modules m WHERE m.business_id = b.id
		             ORDER BY array_position(ARRAY['CATALOG', 'INVENTORY', 'SALES', 'PURCHASE', 'FINANCE', 'BOOKING', 'REPORTING'], module_code))
		FROM business_members bm
		JOIN businesses b ON b.id = bm.business_id AND b.status = 'ACTIVE'
		JOIN roles r ON r.id = bm.role_id AND r.business_id = b.id
		LEFT JOIN locations l ON l.business_id = b.id AND l.is_default AND l.status = 'ACTIVE'
		WHERE bm.user_id = $1 AND bm.status = 'ACTIVE' AND ` + predicate
	err := q.QueryRow(ctx, query, userID, value).Scan(
		&item.ID, &item.Code, &item.Name, &item.BusinessType, &item.Timezone, &item.Currency,
		&item.Role, &locationCode, &locationName, &item.EnabledModules,
	)
	if err != nil {
		return app.BusinessContext{}, mapError(err)
	}
	item.Business.Role = item.Role
	if locationCode != nil && locationName != nil {
		item.DefaultLocation = &app.Location{Code: *locationCode, Name: *locationName}
	}
	return item, nil
}

func (r *Repository) ListProducts(ctx context.Context, businessID string, search string) ([]app.Product, error) {
	query := `
		SELECT p.public_code, p.name, COALESCE(p.sku, ''), COALESCE(p.barcode, ''), u.symbol,
		       p.default_purchase_price::text, p.default_selling_price::text, p.min_stock::text,
		       p.is_stock_tracked, p.status, COALESCE(c.public_code,''), COALESCE(c.name,'')
		FROM products p
		JOIN units u ON u.id = p.base_unit_id AND u.business_id = p.business_id
		LEFT JOIN categories c ON c.id = p.category_id AND c.business_id = p.business_id
		WHERE p.business_id = $1`
	args := []any{businessID}
	if search != "" {
		query += ` AND (p.name ILIKE $2 OR p.sku ILIKE $2 OR p.public_code ILIKE $2)`
		args = append(args, "%"+search+"%")
	}
	query += ` ORDER BY p.name, p.public_code`

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]app.Product, 0)
	for rows.Next() {
		var item app.Product
		if err := rows.Scan(&item.Code, &item.Name, &item.SKU, &item.Barcode, &item.UnitSymbol,
			&item.DefaultPurchasePrice, &item.DefaultSellingPrice, &item.MinStock,
			&item.IsStockTracked, &item.Status, &item.CategoryCode, &item.CategoryName); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) CreateProduct(ctx context.Context, businessID string, input app.NewProduct) (app.Product, error) {
	purchase, err := numeric(input.DefaultPurchasePrice)
	if err != nil {
		return app.Product{}, err
	}
	selling, err := numeric(input.DefaultSellingPrice)
	if err != nil {
		return app.Product{}, err
	}
	minStock, err := numeric(input.MinStock)
	if err != nil {
		return app.Product{}, err
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return app.Product{}, err
	}
	defer rollback(ctx, tx)
	var unitID, unitSymbol, categoryID string
	err = tx.QueryRow(ctx, `
		SELECT id::text, symbol FROM units
		WHERE business_id = $1 AND upper(symbol) = upper($2)`, businessID, input.BaseUnitSymbol,
	).Scan(&unitID, &unitSymbol)
	if err != nil {
		return app.Product{}, mapError(err)
	}
	if input.CategoryCode != "" {
		err = tx.QueryRow(ctx, `SELECT id::text FROM categories WHERE business_id=$1 AND public_code=$2 AND category_type='PRODUCT' AND status='ACTIVE'`, businessID, input.CategoryCode).Scan(&categoryID)
		if err != nil {
			return app.Product{}, mapError(err)
		}
	}
	code, err := nextNumber(ctx, tx, businessID, "PRODUCT")
	if err != nil {
		return app.Product{}, err
	}
	item := app.Product{Code: code, Name: input.Name, SKU: input.SKU, Barcode: input.Barcode, UnitSymbol: unitSymbol, IsStockTracked: input.IsStockTracked, Status: "ACTIVE"}
	err = tx.QueryRow(ctx, `
		INSERT INTO products (
			business_id, public_code, base_unit_id, name, sku, barcode,
			default_purchase_price, default_selling_price, min_stock, is_stock_tracked, category_id
		) VALUES ($1, $2, $3, $4, NULLIF($5, ''), NULLIF($6, ''), $7, $8, $9, $10, NULLIF($11,'')::uuid)
		RETURNING default_purchase_price::text, default_selling_price::text, min_stock::text`,
		businessID, code, unitID, input.Name, input.SKU, input.Barcode,
		purchase, selling, minStock, input.IsStockTracked, categoryID,
	).Scan(&item.DefaultPurchasePrice, &item.DefaultSellingPrice, &item.MinStock)
	if err != nil {
		return app.Product{}, mapError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return app.Product{}, err
	}
	return item, nil
}

func (r *Repository) UpdateProduct(ctx context.Context, businessID, code string, input app.NewProduct) (app.Product, error) {
	purchase, err := numeric(input.DefaultPurchasePrice)
	if err != nil {
		return app.Product{}, err
	}
	selling, err := numeric(input.DefaultSellingPrice)
	if err != nil {
		return app.Product{}, err
	}
	minStock, err := numeric(input.MinStock)
	if err != nil {
		return app.Product{}, err
	}
	var unitID, unitSymbol, categoryID string
	err = r.pool.QueryRow(ctx, `
		SELECT id::text, symbol FROM units
		WHERE business_id = $1 AND upper(symbol) = upper($2)`, businessID, input.BaseUnitSymbol,
	).Scan(&unitID, &unitSymbol)
	if err != nil {
		return app.Product{}, mapError(err)
	}
	if input.CategoryCode != "" {
		err = r.pool.QueryRow(ctx, `SELECT id::text FROM categories WHERE business_id=$1 AND public_code=$2 AND category_type='PRODUCT' AND status='ACTIVE'`, businessID, input.CategoryCode).Scan(&categoryID)
		if err != nil {
			return app.Product{}, mapError(err)
		}
	}

	item := app.Product{Code: code, Name: input.Name, SKU: input.SKU, Barcode: input.Barcode, UnitSymbol: unitSymbol, IsStockTracked: input.IsStockTracked, Status: "ACTIVE"}
	err = r.pool.QueryRow(ctx, `
		UPDATE products SET
			base_unit_id = $1, name = $2, sku = NULLIF($3, ''), barcode = NULLIF($4, ''),
		default_purchase_price = $5, default_selling_price = $6, min_stock = $7, is_stock_tracked = $8, category_id = NULLIF($9,'')::uuid
		WHERE business_id = $10 AND public_code = $11 AND status = 'ACTIVE'
		RETURNING default_purchase_price::text, default_selling_price::text, min_stock::text`,
		unitID, input.Name, input.SKU, input.Barcode,
		purchase, selling, minStock, input.IsStockTracked, categoryID,
		businessID, code,
	).Scan(&item.DefaultPurchasePrice, &item.DefaultSellingPrice, &item.MinStock)
	if err != nil {
		return app.Product{}, mapError(err)
	}
	return item, nil
}

func (r *Repository) DeleteProduct(ctx context.Context, businessID, code, userID string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var pID string
	err = tx.QueryRow(ctx, `SELECT id::text FROM products WHERE business_id = $1 AND public_code = $2`, businessID, code).Scan(&pID)
	if err != nil {
		return mapError(err)
	}

	cmd, err := tx.Exec(ctx, `DELETE FROM products WHERE id = $1`, pID)
	if err != nil {
		return mapError(err)
	}
	if cmd.RowsAffected() == 0 {
		return app.ErrNotFound
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO audit_logs (business_id, actor_user_id, entity_type, entity_id, entity_code, action, after_data)
		VALUES ($1, $2, 'PRODUCT', $3, $4, 'DELETE', '{}')
	`, businessID, userID, pID, code)
	if err != nil {
		return mapError(err)
	}

	return tx.Commit(ctx)
}

func (r *Repository) CreateOpeningStock(ctx context.Context, businessID, userID string, input app.NewOpeningStock) (app.OpeningStock, error) {
	quantity, err := numeric(input.Quantity)
	if err != nil {
		return app.OpeningStock{}, err
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return app.OpeningStock{}, err
	}
	defer rollback(ctx, tx)

	var productID, unitID string
	var tracked bool
	err = tx.QueryRow(ctx, `
		SELECT id::text, base_unit_id::text, is_stock_tracked
		FROM products
		WHERE business_id = $1 AND public_code = $2 AND status = 'ACTIVE'
		FOR UPDATE`, businessID, input.ProductCode,
	).Scan(&productID, &unitID, &tracked)
	if err != nil {
		return app.OpeningStock{}, mapError(err)
	}
	if !tracked {
		return app.OpeningStock{}, app.ErrForbidden
	}
	var locationID string
	err = tx.QueryRow(ctx, `
		SELECT id::text FROM locations
		WHERE business_id = $1 AND public_code = $2 AND status = 'ACTIVE'`, businessID, input.LocationCode,
	).Scan(&locationID)
	if err != nil {
		return app.OpeningStock{}, mapError(err)
	}
	number, err := nextNumber(ctx, tx, businessID, "OPENING_STOCK")
	if err != nil {
		return app.OpeningStock{}, err
	}
	var adjustmentID string
	err = tx.QueryRow(ctx, `
		INSERT INTO stock_adjustments (
			business_id, location_id, adjustment_number, reason, status, notes, created_by
		) VALUES ($1, $2, $3, 'OPENING_BALANCE', 'COMPLETED', NULLIF($4, ''), $5)
		RETURNING id::text`, businessID, locationID, number, input.Reason, userID,
	).Scan(&adjustmentID)
	if err != nil {
		return app.OpeningStock{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO stock_adjustment_items (
			business_id, stock_adjustment_id, product_id, quantity, unit_id, base_quantity, base_unit_id, direction
		) VALUES ($1, $2, $3, $4, $5, $4, $5, 'IN')`, businessID, adjustmentID, productID, quantity, unitID); err != nil {
		return app.OpeningStock{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO stock_movements (
			business_id, product_id, location_id, movement_type, direction,
			quantity, unit_id, base_quantity, base_unit_id, reference_id, reason, created_by
		) VALUES ($1, $2, $3, 'OPENING_BALANCE', 'IN', $4, $5, $4, $5, $6, NULLIF($7, ''), $8)`,
		businessID, productID, locationID, quantity, unitID, adjustmentID, input.Reason, userID); err != nil {
		return app.OpeningStock{}, mapError(err)
	}
	result := app.OpeningStock{AdjustmentNumber: number, ProductCode: input.ProductCode, LocationCode: input.LocationCode, Quantity: input.Quantity}
	err = tx.QueryRow(ctx, `
		INSERT INTO product_inventory (business_id, product_id, location_id, quantity, base_unit_id)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (business_id, product_id, location_id)
		DO UPDATE SET quantity = product_inventory.quantity + EXCLUDED.quantity, updated_at = now()
		RETURNING quantity::text`, businessID, productID, locationID, quantity, unitID,
	).Scan(&result.CurrentQuantity)
	if err != nil {
		return app.OpeningStock{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_logs (
			business_id, actor_user_id, entity_type, entity_id, entity_code, action, after_data, reason
		) VALUES ($1, $2, 'STOCK_ADJUSTMENT', $3, $4, 'OPENING_STOCK_RECORDED',
		          jsonb_build_object('product_code', $5::text, 'location_code', $6::text,
		                             'quantity', $7::text, 'current_quantity', $8::text), NULLIF($9, ''))`,
		businessID, userID, adjustmentID, number, input.ProductCode, input.LocationCode,
		result.Quantity, result.CurrentQuantity, input.Reason); err != nil {
		return app.OpeningStock{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return app.OpeningStock{}, err
	}
	return result, nil
}

func (r *Repository) ListInventoryProducts(ctx context.Context, businessID string, search string) ([]app.InventoryProduct, error) {
	query := `
		SELECT p.public_code, p.name, COALESCE(p.sku, ''), u.symbol,
		       COALESCE(pi.quantity, 0)::text, p.min_stock::text,
		       l.public_code, l.name
		FROM products p
		JOIN units u ON u.id = p.base_unit_id AND u.business_id = p.business_id
		CROSS JOIN locations l
		LEFT JOIN product_inventory pi
		  ON pi.business_id = p.business_id AND pi.product_id = p.id AND pi.location_id = l.id
		WHERE p.business_id = $1 AND p.status = 'ACTIVE' AND p.is_stock_tracked
		  AND l.business_id = $1 AND l.status = 'ACTIVE'`
	args := []any{businessID}
	if search != "" {
		query += ` AND (p.name ILIKE $2 OR p.sku ILIKE $2 OR p.public_code ILIKE $2)`
		args = append(args, "%"+search+"%")
	}
	query += ` ORDER BY p.name, l.name`

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]app.InventoryProduct, 0)
	for rows.Next() {
		var item app.InventoryProduct
		if err := rows.Scan(&item.ProductCode, &item.Name, &item.SKU, &item.UnitSymbol,
			&item.Quantity, &item.MinStock, &item.LocationCode, &item.LocationName); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) ListStockMovements(ctx context.Context, businessID string) ([]app.StockMovement, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT sm.movement_type, sm.direction, sm.quantity::text, u.symbol,
		       p.public_code, p.name, l.public_code, l.name, sm.reason, sm.occurred_at,
		       COALESCE(us.name, '')
		FROM stock_movements sm
		JOIN products p ON sm.product_id = p.id
		JOIN locations l ON sm.location_id = l.id
		JOIN units u ON sm.unit_id = u.id
		LEFT JOIN users us ON sm.created_by = us.id
		WHERE sm.business_id = $1
		ORDER BY sm.occurred_at DESC LIMIT 100`, businessID,
	)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()

	var result []app.StockMovement
	for rows.Next() {
		var m app.StockMovement
		if err := rows.Scan(
			&m.MovementType, &m.Direction, &m.Quantity, &m.UnitSymbol,
			&m.ProductCode, &m.ProductName, &m.LocationCode, &m.LocationName,
			&m.Reason, &m.OccurredAt, &m.CreatedByName,
		); err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	if result == nil {
		result = []app.StockMovement{}
	}
	return result, nil
}

func (r *Repository) CreateStockAdjustment(ctx context.Context, businessID, userID string, input app.NewStockAdjustment) (app.StockAdjustment, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return app.StockAdjustment{}, err
	}
	defer rollback(ctx, tx)

	var locationID string
	err = tx.QueryRow(ctx, `
		SELECT id::text FROM locations
		WHERE business_id = $1 AND public_code = $2 AND status = 'ACTIVE'`, businessID, input.LocationCode,
	).Scan(&locationID)
	if err != nil {
		return app.StockAdjustment{}, mapError(err)
	}

	number, err := nextNumber(ctx, tx, businessID, "STOCK_ADJUSTMENT")
	if err != nil {
		return app.StockAdjustment{}, err
	}

	var adjustmentID string
	err = tx.QueryRow(ctx, `
		INSERT INTO stock_adjustments (
			business_id, location_id, adjustment_number, reason, status, notes, created_by
		) VALUES ($1, $2, $3, $4, 'DRAFT', NULLIF($5, ''), $6)
		RETURNING id::text`, businessID, locationID, number, input.Reason, input.Notes, userID,
	).Scan(&adjustmentID)
	if err != nil {
		return app.StockAdjustment{}, mapError(err)
	}

	var resultItems []app.StockAdjustmentItem
	for _, item := range input.Items {
		quantity, err := numeric(item.Quantity)
		if err != nil {
			return app.StockAdjustment{}, err
		}

		var productID, productName, unitID string
		err = tx.QueryRow(ctx, `
			SELECT p.id::text, p.name, u.id::text
			FROM products p
			JOIN units u ON u.business_id = p.business_id AND upper(u.symbol) = upper($3)
			WHERE p.business_id = $1 AND p.public_code = $2 AND p.status = 'ACTIVE' AND p.is_stock_tracked = true`,
			businessID, item.ProductCode, item.UnitSymbol,
		).Scan(&productID, &productName, &unitID)
		if err != nil {
			return app.StockAdjustment{}, mapError(err)
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO stock_adjustment_items (
				business_id, stock_adjustment_id, product_id, quantity, unit_id, base_quantity, base_unit_id, direction
			) VALUES ($1, $2, $3, $4, $5, $4, $5, $6)`,
			businessID, adjustmentID, productID, quantity, unitID, item.Direction,
		); err != nil {
			return app.StockAdjustment{}, mapError(err)
		}

		resultItems = append(resultItems, app.StockAdjustmentItem{
			ProductCode: item.ProductCode,
			ProductName: productName,
			Quantity:    item.Quantity,
			UnitSymbol:  item.UnitSymbol,
			Direction:   item.Direction,
		})
	}

	if err := tx.Commit(ctx); err != nil {
		return app.StockAdjustment{}, err
	}

	return app.StockAdjustment{
		AdjustmentNumber: number,
		LocationCode:     input.LocationCode,
		Reason:           input.Reason,
		Status:           "DRAFT",
		Notes:            input.Notes,
		AdjustmentDate:   time.Now(),
		Items:            resultItems,
	}, nil
}

func (r *Repository) CompleteStockAdjustment(ctx context.Context, businessID, userID, adjustmentNumber string) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer rollback(ctx, tx)

	var adjustmentID, locationID, locationCode, reason string
	err = tx.QueryRow(ctx, `
		SELECT sa.id::text, sa.location_id::text, l.public_code, sa.reason
		FROM stock_adjustments sa
		JOIN locations l ON sa.location_id = l.id
		WHERE sa.business_id = $1 AND sa.adjustment_number = $2 AND sa.status = 'DRAFT'
		FOR UPDATE OF sa`, businessID, adjustmentNumber,
	).Scan(&adjustmentID, &locationID, &locationCode, &reason)
	if err != nil {
		return mapError(err)
	}

	rows, err := tx.Query(ctx, `
		SELECT product_id::text, p.public_code, quantity::text, unit_id::text, direction
		FROM stock_adjustment_items sai
		JOIN products p ON sai.product_id = p.id
		WHERE sai.business_id = $1 AND sai.stock_adjustment_id = $2`, businessID, adjustmentID,
	)
	if err != nil {
		return mapError(err)
	}

	type itemData struct {
		productID   string
		productCode string
		quantity    string
		unitID      string
		direction   string
	}
	var items []itemData
	for rows.Next() {
		var i itemData
		if err := rows.Scan(&i.productID, &i.productCode, &i.quantity, &i.unitID, &i.direction); err != nil {
			rows.Close()
			return err
		}
		items = append(items, i)
	}
	rows.Close()

	for _, item := range items {
		if _, err := tx.Exec(ctx, `
			INSERT INTO stock_movements (
				business_id, product_id, location_id, movement_type, direction,
				quantity, unit_id, base_quantity, base_unit_id, reference_id, reason, created_by
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $6, $7, $8, 'Stock Adjustment Completed', $9)`,
			businessID, item.productID, locationID, reason, item.direction, item.quantity, item.unitID, adjustmentID, userID,
		); err != nil {
			return mapError(err)
		}

		multiplier := 1
		if item.direction == "OUT" {
			multiplier = -1
		}

		var newQty string
		err = tx.QueryRow(ctx, `
			INSERT INTO product_inventory (business_id, product_id, location_id, quantity, base_unit_id)
			VALUES ($1, $2, $3, $4 * $6, $5)
			ON CONFLICT (business_id, product_id, location_id)
			DO UPDATE SET quantity = product_inventory.quantity + (EXCLUDED.quantity), updated_at = now()
			RETURNING quantity::text`, businessID, item.productID, locationID, item.quantity, item.unitID, multiplier,
		).Scan(&newQty)
		if err != nil {
			return err
		}

		if item.direction == "OUT" {
			var checkQty float64
			if err := r.pool.QueryRow(ctx, `SELECT $1::numeric`, newQty).Scan(&checkQty); err == nil && checkQty < 0 {
				return errors.New("stok tidak mencukupi untuk dikeluarkan")
			}
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO audit_logs (
				business_id, actor_user_id, entity_type, entity_id, entity_code, action, after_data
			) VALUES ($1, $2, 'STOCK_ADJUSTMENT', $3, $4, 'STOCK_ADJUSTED',
					  jsonb_build_object('product_code', $5::text, 'location_code', $6::text,
										 'quantity_diff', $7::text, 'direction', $8::text, 'new_quantity', $9::text))`,
			businessID, userID, adjustmentID, adjustmentNumber, item.productCode, locationCode, item.quantity, item.direction, newQty,
		); err != nil {
			return err
		}
	}

	if _, err := tx.Exec(ctx, `
		UPDATE stock_adjustments SET status = 'COMPLETED', updated_at = now()
		WHERE business_id = $1 AND id = $2`, businessID, adjustmentID,
	); err != nil {
		return mapError(err)
	}

	return tx.Commit(ctx)
}

func nextNumber(ctx context.Context, tx pgx.Tx, businessID, sequenceType string) (string, error) {
	var prefix string
	var value int64
	var padding int
	err := tx.QueryRow(ctx, `
		UPDATE number_sequences
		SET last_number = last_number + 1, updated_at = now()
		WHERE business_id = $1 AND sequence_type = $2
		RETURNING prefix, last_number, padding`, businessID, sequenceType,
	).Scan(&prefix, &value, &padding)
	if err != nil {
		return "", mapError(err)
	}
	return fmt.Sprintf("%s-%0*d", prefix, padding, value), nil
}

func numeric(raw string) (pgtype.Numeric, error) {
	var value pgtype.Numeric
	if err := value.Scan(raw); err != nil {
		return pgtype.Numeric{}, fmt.Errorf("parse numeric value: %w", err)
	}
	return value, nil
}

func rollback(ctx context.Context, tx pgx.Tx) { _ = tx.Rollback(ctx) }

func mapError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return app.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && (pgErr.Code == "23505" || pgErr.Code == "23503") {
		return app.ErrConflict
	}
	return err
}
