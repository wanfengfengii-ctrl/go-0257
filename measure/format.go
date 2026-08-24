package measure

import "fmt"

// FormatPercent renders a basis-point value as a human-readable percentage
// string with two decimal places. 1250 becomes "12.50%", 9800 becomes
// "98.00%". It is used by the inspection report and console.
func FormatPercent(bp Fixed) string {
	if bp < 0 {
		return "-" + FormatPercent(-bp)
	}
	whole := int64(bp) / int64(Scale)
	frac := int64(bp) % int64(Scale)
	return fmt.Sprintf("%d.%02d%%", whole, frac)
}

// FormatBp renders a raw basis-point integer (not necessarily a Fixed) as a
// percentage string, for values read directly from evidence tables.
func FormatBp(bp int64) string {
	return FormatPercent(Fixed(bp))
}

// FormatWeight renders a thousand-grain weight integer in grams with three
// decimal places (the raw integer is stored in milligrams).
func FormatWeight(mg int64) string {
	whole := mg / 1000
	frac := mg % 1000
	return fmt.Sprintf("%d.%03dg", whole, frac)
}
