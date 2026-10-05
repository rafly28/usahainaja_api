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
	UpdateBusinessConfiguration(context.Context, string, string, string, []string) error
	ListBusinessMembers(context.Context, string) ([]BusinessMember, error)
	InviteBusinessMember(context.Context, string, string, string, string) (BusinessMember, error)
	UpdateBusinessMember(context.Context, string, string, string, string, string, string) (BusinessMember, error)
	ListCategories(context.Context, string, string) ([]Category, error)
	CreateCategory(context.Context, string, string, NewCategory) (Category, error)
	UpdateCategory(context.Context, string, string, string, string, NewCategory) (Category, error)
	ListUnits(context.Context, string) ([]Unit, error)
	CreateUnit(context.Context, string, string, NewUnit) (Unit, error)
	UpdateUnit(context.Context, string, string, string, string, NewUnit) (Unit, error)
	ListUnitConversions(context.Context, string) ([]UnitConversion, error)
	CreateUnitConversion(context.Context, string, string, NewUnitConversion) (UnitConversion, error)
	ListLocations(context.Context, string) ([]Location, error)
	CreateLocation(context.Context, string, string, NewLocation) (Location, error)
	UpdateLocation(context.Context, string, string, string, string, NewLocation) (Location, error)
	ListParties(context.Context, string) ([]Party, error)
	CreateParty(context.Context, string, string, NewParty) (Party, error)
	UpdateParty(context.Context, string, string, string, string, NewParty) (Party, error)

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

	ListSales(ctx context.Context, businessID string, page, limit int) (items []Sale, total int, err error)
	CreateSale(context.Context, string, string, NewSale) (Sale, error)
	GetSale(ctx context.Context, businessID, receiptNumber string) (Sale, error)
	AddSaleItem(ctx context.Context, businessID, userID, receiptNumber string, item NewSaleItem) (SaleItem, error)
	UpdateSaleItem(ctx context.Context, businessID, userID, receiptNumber string, lineNumber int, item NewSaleItem) (SaleItem, error)
	DeleteSaleItem(ctx context.Context, businessID, userID, receiptNumber string, lineNumber int) error
	CancelSale(ctx context.Context, businessID, userID, receiptNumber string) error
	CheckoutSale(ctx context.Context, businessID, userID, receiptNumber string, paymentInput PaymentInput) (Sale, error)
	VoidSale(ctx context.Context, businessID, userID, receiptNumber, reason string) error

	ListPurchases(ctx context.Context, businessID string, page, limit int) (items []Purchase, total int, err error)
	CreatePurchase(context.Context, string, string, NewPurchase) (Purchase, error)
	GetPurchase(ctx context.Context, businessID, purchaseNumber string) (Purchase, error)
	AddPurchaseItem(ctx context.Context, businessID, userID, purchaseNumber string, item NewPurchaseItem) (PurchaseItem, error)
	UpdatePurchaseItem(ctx context.Context, businessID, userID, purchaseNumber string, lineNumber int, item NewPurchaseItem) (PurchaseItem, error)
	DeletePurchaseItem(ctx context.Context, businessID, userID, purchaseNumber string, lineNumber int) error
	OrderPurchase(ctx context.Context, businessID, userID, purchaseNumber string) error
	CancelPurchase(ctx context.Context, businessID, userID, purchaseNumber string) error
	ReceivePurchase(ctx context.Context, businessID, purchaseNumber, userID string, in ReceivePurchaseInput) (PurchaseReceipt, error)
	RecordPurchasePayment(ctx context.Context, businessID, purchaseNumber, userID string, in PaymentInput) (Payment, error)

	FindIdempotency(ctx context.Context, businessID, key string) (status int, body []byte, hash string, found bool, err error)
	StoreIdempotency(ctx context.Context, businessID, actorID, operation, route, key, hash string, status int, body []byte) error
}
