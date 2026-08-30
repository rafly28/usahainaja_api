package app

import "context"

type Repository interface {
	Ping(context.Context) error
	CreateUserAndSession(context.Context, NewUser, NewSession, []byte) (UserRecord, Session, error)
	FindUserByEmail(context.Context, string) (UserRecord, error)
	ReplaceSession(context.Context, string, NewSession, []byte) (Session, error)
	LoadSession(context.Context, []byte) (Session, error)
	DeleteSession(context.Context, string) error

	ListBusinesses(context.Context, string) ([]Business, error)
	CreateBusiness(context.Context, string, string, NewBusiness) (BusinessContext, error)
	GetBusinessContext(context.Context, string, string) (BusinessContext, error)
	SwitchBusiness(context.Context, string, string, string) (BusinessContext, error)

	ListProducts(ctx context.Context, businessID string, search string) ([]Product, error)
	CreateProduct(ctx context.Context, businessID string, input NewProduct) (Product, error)
	UpdateProduct(ctx context.Context, businessID, code string, input NewProduct) (Product, error)
	DeleteProduct(ctx context.Context, businessID, code, userID string) error

	ListInventoryProducts(ctx context.Context, businessID string, search string) ([]InventoryProduct, error)
	CreateOpeningStock(ctx context.Context, businessID, userID string, input NewOpeningStock) (OpeningStock, error)
	ListStockMovements(ctx context.Context, businessID string) ([]StockMovement, error)
	CreateStockAdjustment(ctx context.Context, businessID, userID string, input NewStockAdjustment) (StockAdjustment, error)
	CompleteStockAdjustment(ctx context.Context, businessID, userID, adjustmentNumber string) error

	ListContacts(context.Context, string) ([]Contact, error)
	CreateContact(context.Context, string, NewContact) (Contact, error)

	ListCashAccounts(context.Context, string) ([]CashAccount, error)
	CreateCashAccount(context.Context, string, NewCashAccount) (CashAccount, error)

	ListSales(context.Context, string) ([]Sale, error)
	CreateSale(context.Context, string, string, NewSale) (Sale, error)

	ListPurchases(context.Context, string) ([]Purchase, error)
	CreatePurchase(context.Context, string, string, NewPurchase) (Purchase, error)
}
