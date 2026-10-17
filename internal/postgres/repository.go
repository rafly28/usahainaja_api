package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

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
		       ($1, 'STOCK_ADJUSTMENT', 'ADJ'), ($1, 'STOCK_OPNAME', 'OPN'),
		       ($1, 'CONTACT', 'CNT'),
		       ($1, 'CASH', 'CSH'), ($1, 'SALE', 'SL'), ($1, 'PURC', 'PUR'),
		       ($1, 'PAY', 'PAY'), ($1, 'PURCHASE_RECEIPT', 'RCV'),
		       ($1, 'CATEGORY', 'CAT'), ($1, 'PARTY', 'PTY'),
		       ($1, 'LOCATION', 'LOC'), ($1, 'UNIT', 'UNT')`, businessID); err != nil {
		return app.BusinessContext{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO payment_methods (business_id, public_code, name, method_type, is_default)
		VALUES ($1, 'CASH', 'Tunai', 'CASH', true)`, businessID); err != nil {
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

// safeCodeSuffix membuat suffix fallback tanpa panic untuk nama pendek/unicode.
func safeCodeSuffix(name string, seed int64) string {
	upper := strings.ToUpper(strings.TrimSpace(name))
	runes := []rune(upper)
	prefix := string(runes)
	if utf8.RuneCountInString(prefix) > 3 {
		prefix = string(runes[:3])
	}
	var b strings.Builder
	for _, r := range prefix {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		b.WriteString("XXX")
	}
	return fmt.Sprintf("%s%d", b.String(), seed%100000)
}

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
