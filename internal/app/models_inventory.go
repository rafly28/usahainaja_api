package app

import "time"

// InventoryProduct adalah ringkasan stok satu produk pada satu lokasi.
// Low stock dihitung di server agar dashboard dan report memakai definisi sama.
type InventoryProduct struct {
	ProductCode  string `json:"product_code"`
	Name         string `json:"name"`
	SKU          string `json:"sku,omitempty"`
	UnitSymbol   string `json:"unit_symbol"`
	Quantity     string `json:"quantity"`
	MinStock     string `json:"min_stock"`
	LocationCode string `json:"location_code"`
	LocationName string `json:"location_name"`
	IsLowStock   bool   `json:"is_low_stock"`
}

// InventoryFilter membatasi daftar stok sesuai query kontrak.
type InventoryFilter struct {
	Search       string
	LocationCode string
	LowStockOnly bool
	Page         int
	Limit        int
}

// NewOpeningStock membuat stok awal batch untuk satu lokasi.
type NewOpeningStock struct {
	LocationCode string
	Reason       string
	Items        []NewOpeningStockItem
}

type NewOpeningStockItem struct {
	ProductCode string
	Quantity    string
	UnitSymbol  string
}

type OpeningStock struct {
	AdjustmentNumber string             `json:"adjustment_number"`
	LocationCode     string             `json:"location_code"`
	Items            []OpeningStockItem `json:"items"`
}

type OpeningStockItem struct {
	ProductCode     string `json:"product_code"`
	ProductName     string `json:"product_name"`
	Quantity        string `json:"quantity"`
	UnitSymbol      string `json:"unit_symbol"`
	CurrentQuantity string `json:"current_quantity"`
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

// StockMovementFilter mengikuti query parameter kontrak riwayat pergerakan.
type StockMovementFilter struct {
	ProductCode  string
	LocationCode string
	MovementType string
	DateFrom     string
	DateTo       string
	Page         int
	Limit        int
}

// StockAdjustment adalah header penyesuaian stok manual, opname, atau stok awal.
type StockAdjustment struct {
	AdjustmentNumber string                `json:"adjustment_number"`
	LocationCode     string                `json:"location_code"`
	Reason           string                `json:"reason"`
	Kind             string                `json:"adjustment_kind"`
	Status           string                `json:"status"`
	Notes            string                `json:"notes,omitempty"`
	AdjustmentDate   time.Time             `json:"adjustment_date"`
	CreatedByName    string                `json:"created_by_name,omitempty"`
	Items            []StockAdjustmentItem `json:"items"`
}

type StockAdjustmentItem struct {
	ProductCode     string `json:"product_code"`
	ProductName     string `json:"product_name"`
	Quantity        string `json:"quantity"`
	UnitSymbol      string `json:"unit_symbol"`
	Direction       string `json:"direction"`
	SystemQuantity  string `json:"system_quantity,omitempty"`
	CountedQuantity string `json:"counted_quantity,omitempty"`
}

// NewStockAdjustment membuat adjustment manual; arah dan kuantitas berasal dari pengguna.
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

// NewStockOpname membuat adjustment hasil hitung fisik. Kuantitas sistem selalu
// dibaca dari server saat transaksi dibuka, bukan dipercaya dari payload.
type NewStockOpname struct {
	LocationCode string               `json:"location_code"`
	Reason       string               `json:"reason"`
	Notes        string               `json:"notes,omitempty"`
	Items        []NewStockOpnameItem `json:"items"`
}

type NewStockOpnameItem struct {
	ProductCode     string `json:"product_code"`
	CountedQuantity string `json:"counted_quantity"`
	UnitSymbol      string `json:"unit_symbol,omitempty"`
}

// StockAdjustmentFilter membatasi daftar adjustment stok per bisnis.
type StockAdjustmentFilter struct {
	Status string
	Kind   string
	Page   int
	Limit  int
}
