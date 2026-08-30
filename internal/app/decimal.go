package app

import (
	"errors"
	"math/big"
	"regexp"
	"strings"
)

var decimalPattern = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?$`)

func normalizeDecimal(raw string, scale, maxIntegerDigits int, strictlyPositive bool) (string, error) {
	raw = strings.TrimSpace(raw)
	if !decimalPattern.MatchString(raw) {
		return "", errors.New("harus berupa angka desimal non-negatif")
	}

	parts := strings.SplitN(raw, ".", 2)
	integer := strings.TrimLeft(parts[0], "0")
	if integer == "" {
		integer = "0"
	}
	if len(integer) > maxIntegerDigits {
		return "", errors.New("nilai terlalu besar")
	}

	fraction := ""
	if len(parts) == 2 {
		fraction = strings.TrimRight(parts[1], "0")
		if len(fraction) > scale {
			return "", errors.New("jumlah angka di belakang koma terlalu banyak")
		}
	}

	normalized := integer
	if fraction != "" {
		normalized += "." + fraction
	}
	value, ok := new(big.Rat).SetString(normalized)
	if !ok {
		return "", errors.New("angka tidak valid")
	}
	if strictlyPositive && value.Sign() <= 0 {
		return "", errors.New("harus lebih besar dari 0")
	}
	return normalized, nil
}
