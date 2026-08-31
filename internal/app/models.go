package app

import (
	"fmt"
	"strings"
	"time"
)

type User struct {
	Code  string `json:"code"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type UserRecord struct {
	ID           string
	User         User
	PasswordHash string
}

type Session struct {
	ID               string
	UserID           string
	User             User
	ActiveBusinessID *string
	CSRFToken        string
	ExpiresAt        time.Time
}

type SessionCredentials struct {
	Token     string
	CSRFToken string
	ExpiresAt time.Time
}

type SessionMeta struct {
	UserAgent string
	IPAddress string
}

type NewSession struct {
	TokenHash []byte
	CSRFToken string
	ExpiresAt time.Time
	Meta      SessionMeta
}

type Location struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

type Business struct {
	Code            string    `json:"code"`
	Name            string    `json:"name"`
	BusinessType    string    `json:"business_type"`
	EnabledModules  []string  `json:"enabled_modules"`
	Timezone        string    `json:"timezone"`
	Currency        string    `json:"currency"`
	Role            string    `json:"role,omitempty"`
	DefaultLocation *Location `json:"default_location,omitempty"`
}

type BusinessContext struct {
	ID   string
	Role string
	Business
}

type NewUser struct {
	Code         string
	Name         string
	Email        string
	PasswordHash string
}

type NewBusiness struct {
	Code           string
	Name           string
	BusinessType   string
	Timezone       string
	Currency       string
	LocationCode   string
	UnitCode       string
	EnabledModules []string
}

type Category struct {
	Code         string  `json:"code"`
	Name         string  `json:"name"`
	CategoryType string  `json:"category_type"`
	ParentCode   *string `json:"parent_code,omitempty"`
	Status       string  `json:"status"`
}

type Unit struct {
	Code     string `json:"code"`
	Name     string `json:"name"`
	Symbol   string `json:"symbol"`
	UnitType string `json:"unit_type"`
	Status   string `json:"status"`
}

type NewUnit struct{ Name, Symbol, UnitType string }

type NewCategory struct {
	Name         string
	CategoryType string
	ParentCode   string
}

const (
	ModuleCatalog   = "CATALOG"
	ModuleInventory = "INVENTORY"
	ModuleSales     = "SALES"
	ModulePurchase  = "PURCHASE"
	ModuleFinance   = "FINANCE"
	ModuleBooking   = "BOOKING"
	ModuleReporting = "REPORTING"
)

var moduleOrder = []string{
	ModuleCatalog,
	ModuleInventory,
	ModuleSales,
	ModulePurchase,
	ModuleFinance,
	ModuleBooking,
	ModuleReporting,
}

// DefaultModulesForBusinessType is the initial module profile created with a
// business. Module configuration may later be changed by an owner or admin.
func DefaultModulesForBusinessType(businessType string) []string {
	switch businessType {
	case "RETAIL":
		return []string{ModuleCatalog, ModuleInventory, ModuleSales, ModulePurchase, ModuleFinance, ModuleReporting}
	case "SERVICE", "ENTERTAINMENT":
		return []string{ModuleBooking, ModuleFinance, ModuleReporting}
	default:
		return []string{}
	}
}

// NormalizeModules validates module codes, de-duplicates them, and returns a
// stable order for API responses and database writes.
func NormalizeModules(modules []string) ([]string, error) {
	selected := make(map[string]struct{}, len(modules))
	for _, module := range modules {
		module = strings.ToUpper(strings.TrimSpace(module))
		if module == "" {
			continue
		}
		valid := false
		for _, allowed := range moduleOrder {
			if module == allowed {
				valid = true
				break
			}
		}
		if !valid {
			return nil, fmt.Errorf("modul %q tidak didukung", module)
		}
		selected[module] = struct{}{}
	}

	result := make([]string, 0, len(selected))
	for _, module := range moduleOrder {
		if _, ok := selected[module]; ok {
			result = append(result, module)
		}
	}
	return result, nil
}

type Product struct {
	Code                 string `json:"code"`
	Name                 string `json:"name"`
	SKU                  string `json:"sku,omitempty"`
	Barcode              string `json:"barcode,omitempty"`
	UnitSymbol           string `json:"unit_symbol"`
	DefaultPurchasePrice string `json:"default_purchase_price"`
	DefaultSellingPrice  string `json:"default_selling_price"`
	MinStock             string `json:"min_stock"`
	IsStockTracked       bool   `json:"is_stock_tracked"`
	Status               string `json:"status"`
}

type NewProduct struct {
	Name                 string
	SKU                  string
	Barcode              string
	BaseUnitSymbol       string
	DefaultPurchasePrice string
	DefaultSellingPrice  string
	MinStock             string
	IsStockTracked       bool
}

type InventoryProduct struct {
	ProductCode  string `json:"product_code"`
	Name         string `json:"name"`
	SKU          string `json:"sku,omitempty"`
	UnitSymbol   string `json:"unit_symbol"`
	Quantity     string `json:"quantity"`
	MinStock     string `json:"min_stock"`
	LocationCode string `json:"location_code"`
	LocationName string `json:"location_name"`
}

type OpeningStock struct {
	AdjustmentNumber string `json:"adjustment_number"`
	ProductCode      string `json:"product_code"`
	LocationCode     string `json:"location_code"`
	Quantity         string `json:"quantity"`
	CurrentQuantity  string `json:"current_quantity"`
}

type NewOpeningStock struct {
	ProductCode  string
	LocationCode string
	Quantity     string
	Reason       string
}

type StockMovement struct {
	MovementType  string    `json:"movement_type"`
	Direction     string    `json:"direction"`
	Quantity      string    `json:"quantity"`
	UnitSymbol    string    `json:"unit_symbol"`
	ProductCode   string    `json:"product_code"`
	ProductName   string    `json:"product_name"`
	LocationCode  string    `json:"location_code"`
	LocationName  string    `json:"location_name"`
	Reason        string    `json:"reason,omitempty"`
	OccurredAt    time.Time `json:"occurred_at"`
	CreatedByName string    `json:"created_by_name,omitempty"`
}

type NewStockAdjustment struct {
	LocationCode string                   `json:"location_code"`
	Reason       string                   `json:"reason"`
	Notes        string                   `json:"notes,omitempty"`
	Items        []NewStockAdjustmentItem `json:"items"`
}

type NewStockAdjustmentItem struct {
	ProductCode string `json:"product_code"`
	Quantity    string `json:"quantity"`
	UnitSymbol  string `json:"unit_symbol"`
	Direction   string `json:"direction"`
}

type StockAdjustment struct {
	AdjustmentNumber string                `json:"adjustment_number"`
	LocationCode     string                `json:"location_code"`
	Reason           string                `json:"reason"`
	Status           string                `json:"status"`
	Notes            string                `json:"notes,omitempty"`
	AdjustmentDate   time.Time             `json:"adjustment_date"`
	Items            []StockAdjustmentItem `json:"items"`
}

type StockAdjustmentItem struct {
	ProductCode string `json:"product_code"`
	ProductName string `json:"product_name"`
	Quantity    string `json:"quantity"`
	UnitSymbol  string `json:"unit_symbol"`
	Direction   string `json:"direction"`
}

type Contact struct {
	Code        string `json:"code"`
	ContactType string `json:"contact_type"`
	Name        string `json:"name"`
	Email       string `json:"email,omitempty"`
	Phone       string `json:"phone,omitempty"`
	Address     string `json:"address,omitempty"`
	Status      string `json:"status"`
}

type NewContact struct {
	ContactType string
	Name        string
	Email       string
	Phone       string
	Address     string
}

type CashAccount struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	AccountType string `json:"account_type"`
	Balance     string `json:"balance"`
	IsDefault   bool   `json:"is_default"`
	Status      string `json:"status"`
}

type NewCashAccount struct {
	Name        string
	AccountType string
	Balance     string
	IsDefault   bool
}

type SaleItem struct {
	ProductCode string `json:"product_code"`
	ProductName string `json:"product_name"`
	Quantity    string `json:"quantity"`
	UnitPrice   string `json:"unit_price"`
	Discount    string `json:"discount"`
	Subtotal    string `json:"subtotal"`
	Notes       string `json:"notes,omitempty"`
}

type Sale struct {
	ReceiptNumber string     `json:"receipt_number"`
	SaleDate      time.Time  `json:"sale_date"`
	LocationCode  string     `json:"location_code"`
	CustomerCode  *string    `json:"customer_code,omitempty"`
	Status        string     `json:"status"`
	PaymentStatus string     `json:"payment_status"`
	Subtotal      string     `json:"subtotal"`
	DiscountTotal string     `json:"discount_total"`
	TaxTotal      string     `json:"tax_total"`
	GrandTotal    string     `json:"grand_total"`
	Notes         string     `json:"notes,omitempty"`
	Items         []SaleItem `json:"items,omitempty"`
}

type NewSaleItem struct {
	ProductCode string `json:"product_code"`
	Quantity    string `json:"quantity"`
	UnitPrice   string `json:"unit_price"`
	Discount    string `json:"discount"`
	Notes       string `json:"notes,omitempty"`
}

type NewSale struct {
	LocationCode  string        `json:"location_code"`
	CustomerCode  string        `json:"customer_code"`
	PaymentStatus string        `json:"payment_status"`
	DiscountTotal string        `json:"discount_total"`
	TaxTotal      string        `json:"tax_total"`
	Notes         string        `json:"notes,omitempty"`
	Items         []NewSaleItem `json:"items"`
}

type PurchaseItem struct {
	ProductCode string `json:"product_code"`
	ProductName string `json:"product_name"`
	Quantity    string `json:"quantity"`
	UnitPrice   string `json:"unit_price"`
	Discount    string `json:"discount"`
	Subtotal    string `json:"subtotal"`
	Notes       string `json:"notes,omitempty"`
}

type Purchase struct {
	PurchaseNumber  string         `json:"purchase_number"`
	ReferenceNumber string         `json:"reference_number,omitempty"`
	PurchaseDate    time.Time      `json:"purchase_date"`
	LocationCode    string         `json:"location_code"`
	SupplierCode    *string        `json:"supplier_code,omitempty"`
	Status          string         `json:"status"`
	PaymentStatus   string         `json:"payment_status"`
	Subtotal        string         `json:"subtotal"`
	DiscountTotal   string         `json:"discount_total"`
	TaxTotal        string         `json:"tax_total"`
	GrandTotal      string         `json:"grand_total"`
	Notes           string         `json:"notes,omitempty"`
	Items           []PurchaseItem `json:"items,omitempty"`
}

type NewPurchaseItem struct {
	ProductCode string `json:"product_code"`
	Quantity    string `json:"quantity"`
	UnitPrice   string `json:"unit_price"`
	Discount    string `json:"discount"`
	Notes       string `json:"notes,omitempty"`
}

type NewPurchase struct {
	LocationCode    string            `json:"location_code"`
	SupplierCode    string            `json:"supplier_code"`
	ReferenceNumber string            `json:"reference_number,omitempty"`
	PaymentStatus   string            `json:"payment_status"`
	DiscountTotal   string            `json:"discount_total"`
	TaxTotal        string            `json:"tax_total"`
	Notes           string            `json:"notes,omitempty"`
	Items           []NewPurchaseItem `json:"items"`
}

type PaymentInput struct {
	CashAccountCode string `json:"cash_account_code"`
	Amount          string `json:"amount"`
	ReferenceNumber string `json:"reference_number,omitempty"`
	Notes           string `json:"notes,omitempty"`
}

type Payment struct {
	PaymentNumber   string    `json:"payment_number"`
	CashAccountCode string    `json:"cash_account_code"`
	PaymentDate     time.Time `json:"payment_date"`
	Amount          string    `json:"amount"`
	ReferenceNumber string    `json:"reference_number,omitempty"`
	Notes           string    `json:"notes,omitempty"`
}
