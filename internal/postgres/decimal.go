package postgres

import "math/big"

// stockScale adalah jumlah digit desimal untuk kuantitas stok agar sama dengan
// numeric(18,4) pada tabel stock_movements, stock_adjustment_items, dan product_inventory.
const stockScale = 4

// multiplyDecimal mengalikan dua angka desimal lalu membulatkan ke scale tertentu.
func multiplyDecimal(a, b string, scale int) (string, error) {
	ra, err := parseRat(a)
	if err != nil {
		return "", err
	}
	rb, err := parseRat(b)
	if err != nil {
		return "", err
	}
	return new(big.Rat).Mul(ra, rb).FloatString(scale), nil
}

// addDecimalScaled menjumlahkan dua angka desimal pada scale tertentu.
func addDecimalScaled(a, b string, scale int) (string, error) {
	ra, err := parseRat(a)
	if err != nil {
		return "", err
	}
	rb, err := parseRat(b)
	if err != nil {
		return "", err
	}
	return new(big.Rat).Add(ra, rb).FloatString(scale), nil
}

// differenceDecimal mengembalikan nilai mutlak selisih a - b pada scale tertentu.
func differenceDecimal(a, b string, scale int) (string, error) {
	ra, err := parseRat(a)
	if err != nil {
		return "", err
	}
	rb, err := parseRat(b)
	if err != nil {
		return "", err
	}
	return new(big.Rat).Abs(new(big.Rat).Sub(ra, rb)).FloatString(scale), nil
}

// decimalIsNegative melaporkan apakah nilai desimal berada di bawah nol.
func decimalIsNegative(raw string) (bool, error) {
	value, err := parseRat(raw)
	if err != nil {
		return false, err
	}
	return value.Sign() < 0, nil
}
