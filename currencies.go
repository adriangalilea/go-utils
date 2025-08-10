package utils

import (
	"math"
)

// currencyOps provides currency-related operations
type currencyOps struct{}

// Currency provides currency formatting and operations
var Currency = &currencyOps{}

// Currency symbols as Unicode characters
const (
	SymbolBTC  = "₿"
	SymbolETH  = "Ξ"
	SymbolUSD  = "$"
	SymbolEUR  = "€"
	SymbolGBP  = "£"
	SymbolJPY  = "¥"
	SymbolCNY  = "¥"
	SymbolKRW  = "₩"
	SymbolINR  = "₹"
	SymbolRUB  = "₽"
	SymbolTRY  = "₺"
	SymbolAUD  = "A$"
	SymbolCAD  = "C$"
	SymbolCHF  = "Fr"
	SymbolHKD  = "HK$"
	SymbolSGD  = "S$"
	SymbolNZD  = "NZ$"
	SymbolSEK  = "kr"
	SymbolNOK  = "kr"
	SymbolDKK  = "kr"
	SymbolPLN  = "zł"
	SymbolTHB  = "฿"
	SymbolUSDT = "₮"
)

// CurrencySymbols maps currency codes to their symbols
var CurrencySymbols = map[string]string{
	"BTC":  SymbolBTC,
	"XBT":  SymbolBTC, // Alternative BTC code
	"ETH":  SymbolETH,
	"USD":  SymbolUSD,
	"EUR":  SymbolEUR,
	"GBP":  SymbolGBP,
	"JPY":  SymbolJPY,
	"CNY":  SymbolCNY,
	"KRW":  SymbolKRW,
	"INR":  SymbolINR,
	"RUB":  SymbolRUB,
	"TRY":  SymbolTRY,
	"AUD":  SymbolAUD,
	"CAD":  SymbolCAD,
	"CHF":  SymbolCHF,
	"HKD":  SymbolHKD,
	"SGD":  SymbolSGD,
	"NZD":  SymbolNZD,
	"SEK":  SymbolSEK,
	"NOK":  SymbolNOK,
	"DKK":  SymbolDKK,
	"PLN":  SymbolPLN,
	"THB":  SymbolTHB,
	"USDT": SymbolUSDT,
	"USDC": SymbolUSD, // Use $ for USDC
	"DAI":  SymbolUSD, // Use $ for DAI
	"BUSD": SymbolUSD, // Use $ for BUSD
}

// GetSymbol returns the symbol for a currency code, or the code itself if not found
func (c *currencyOps) GetSymbol(code string) string {
	if symbol, exists := CurrencySymbols[code]; exists {
		return symbol
	}
	return code
}

// GetOptimalDecimals returns optimal decimal places based on value magnitude and currency
func (c *currencyOps) GetOptimalDecimals(value float64, currencyCode string) int {
	if value == 0 {
		// For zero, use currency defaults
		if c.IsCrypto(currencyCode) {
			return 8
		}
		return 2
	}
	
	absValue := math.Abs(value)
	
	// Special handling for specific currencies
	switch currencyCode {
	case "BTC", "XBT":
		// Bitcoin pairs need maximum precision
		if absValue < 0.00001 {
			return 10
		} else if absValue < 0.0001 {
			return 9
		} else if absValue < 0.001 {
			return 8
		} else if absValue < 0.01 {
			return 7
		} else if absValue < 0.1 {
			return 6
		} else if absValue < 1 {
			return 5
		} else {
			return 4
		}
		
	case "ETH":
		// Ethereum needs good precision
		if absValue < 0.001 {
			return 8
		} else if absValue < 0.01 {
			return 7
		} else if absValue < 0.1 {
			return 6
		} else if absValue < 1 {
			return 5
		} else {
			return 4
		}
		
	case "USD", "USDT", "USDC", "DAI", "BUSD":
		// USD and stablecoins
		if absValue < 0.01 {
			return 6
		} else if absValue < 0.1 {
			return 4
		} else if absValue < 1 {
			return 3
		} else if absValue < 1000 {
			return 2
		} else {
			return 2
		}
		
	case "EUR", "GBP", "CAD", "AUD", "CHF":
		// Major fiat currencies
		if absValue < 0.01 {
			return 4
		} else if absValue < 1000 {
			return 2
		} else {
			return 0
		}
		
	case "JPY", "KRW":
		// Currencies typically without decimals
		if absValue < 1 {
			return 2
		} else {
			return 0
		}
	}
	
	// Default logic for other currencies/values
	if c.IsCrypto(currencyCode) {
		// Crypto defaults
		if absValue < 0.00001 {
			return 8
		} else if absValue < 0.0001 {
			return 6
		} else if absValue < 0.001 {
			return 5
		} else if absValue < 0.01 {
			return 4
		} else if absValue < 0.1 {
			return 3
		} else if absValue < 1 {
			return 3
		} else if absValue < 100 {
			return 2
		} else {
			return 0
		}
	} else {
		// Fiat defaults
		if absValue < 0.01 {
			return 4
		} else if absValue < 0.1 {
			return 3
		} else if absValue < 1000 {
			return 2
		} else if absValue < 100000 {
			return 0
		} else {
			return 0
		}
	}
}

// IsCrypto returns true if the currency code is a known cryptocurrency
func (c *currencyOps) IsCrypto(code string) bool {
	switch code {
	case "BTC", "XBT", "ETH", "BNB", "XRP", "ADA", "DOGE", "SOL", "DOT", "MATIC", 
		 "SHIB", "TRX", "AVAX", "UNI", "ATOM", "LINK", "XMR", "XLM", "ALGO", "VET", 
		 "MANA", "SAND", "AXS", "THETA", "FTM", "NEAR", "HNT", "GRT", "ENJ", "CHZ":
		return true
	default:
		return false
	}
}

// IsStablecoin returns true if the currency is a stablecoin
func (c *currencyOps) IsStablecoin(code string) bool {
	switch code {
	case "USDT", "USDC", "DAI", "BUSD", "UST", "TUSD", "USDP", "GUSD", "FRAX", "LUSD":
		return true
	default:
		return false
	}
}

// IsFiat returns true if the currency is a fiat currency
func (c *currencyOps) IsFiat(code string) bool {
	switch code {
	case "USD", "EUR", "GBP", "JPY", "CNY", "CAD", "AUD", "CHF", "HKD", "SGD",
		 "NZD", "KRW", "SEK", "NOK", "DKK", "PLN", "THB", "INR", "RUB", "TRY",
		 "BRL", "MXN", "ARS", "CLP", "COP", "PEN", "UYU", "ZAR", "NGN", "KES":
		return true
	default:
		return false
	}
}

// Percentage calculation helpers

// PercentageOf calculates what percentage value is of total
func (c *currencyOps) PercentageOf(value, total float64) float64 {
	if total == 0 {
		return 0
	}
	return (value / total) * 100
}

// PercentageChange calculates the percentage change from old to new value
func (c *currencyOps) PercentageChange(oldValue, newValue float64) float64 {
	if oldValue == 0 {
		if newValue == 0 {
			return 0
		}
		// Can't calculate percentage change from zero
		// Return 100% if new value is positive, -100% if negative
		if newValue > 0 {
			return 100
		}
		return -100
	}
	return ((newValue - oldValue) / math.Abs(oldValue)) * 100
}

// PercentageDiff calculates the percentage difference between two values
// This is symmetric: PercentageDiff(a, b) == PercentageDiff(b, a)
func (c *currencyOps) PercentageDiff(a, b float64) float64 {
	if a == 0 && b == 0 {
		return 0
	}
	avg := (math.Abs(a) + math.Abs(b)) / 2
	if avg == 0 {
		return 0
	}
	return (math.Abs(a - b) / avg) * 100
}

// BasisPointsToPercent converts basis points to percentage (100 bps = 1%)
func (c *currencyOps) BasisPointsToPercent(bps int) float64 {
	return float64(bps) / 100.0
}

// PercentToBasisPoints converts percentage to basis points (1% = 100 bps)
func (c *currencyOps) PercentToBasisPoints(percent float64) int {
	return int(percent * 100)
}

// FormatBasisPoints formats basis points as a string with "bps" suffix
func (c *currencyOps) FormatBasisPoints(bps int) string {
	return String(bps) + " bps"
}