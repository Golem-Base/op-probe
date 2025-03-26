package internal

import (
	"fmt"
	"math/big"
	"strings"
)

func FormatWei(amount *big.Int) string {
	return FormatBigInt(amount, 18) // Ethereum uses 18 decimals
}

// FormatBigInt is a generic function to format any big integer with the specified
// number of base decimals, always showing all decimal places
func FormatBigInt(amount *big.Int, baseDecimals int) string {
	if amount == nil {
		return "0"
	}

	// Create a copy of the amount to avoid modifying the original
	value := new(big.Int).Set(amount)

	// Handle negative values
	negative := value.Sign() < 0
	if negative {
		value.Abs(value)
	}

	// Convert to a decimal representation
	// First, get the integer part by dividing by 10^baseDecimals
	divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(baseDecimals)), nil)
	intPart := new(big.Int).Div(value, divisor)

	// Get the fractional part
	remainder := new(big.Int).Mod(value, divisor)

	// If there's no fractional part, just return the integer part
	if remainder.Cmp(big.NewInt(0)) == 0 {
		if negative {
			return "-" + intPart.String()
		}
		return intPart.String()
	}

	// Convert remainder to a string padded with leading zeros
	remainderStr := remainder.String()
	paddedRemainderStr := strings.Repeat("0", baseDecimals-len(remainderStr)) + remainderStr

	// Trim trailing zeros
	trimmedDecimal := strings.TrimRight(paddedRemainderStr, "0")

	// Format the final string
	var result string
	if trimmedDecimal != "" {
		result = fmt.Sprintf("%s.%s", intPart.String(), trimmedDecimal)
	} else {
		result = intPart.String()
	}

	if negative {
		return "-" + result
	}
	return result
}
