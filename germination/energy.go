package germination

import (
	"riceguard/blindcode"
	"riceguard/domain"
)

// EnergyDay returns the observation day age used for the germination-energy
// (发芽势) computation: the earliest day in the locked schedule, representing
// early seedling vigor.
func EnergyDay(dayAges []int32) DayAge {
	if len(dayAges) == 0 {
		return 0
	}
	min := dayAges[0]
	for _, d := range dayAges[1:] {
		if d < min {
			min = d
		}
	}
	return DayAge(min)
}

// FinalDay returns the observation day age used for the germination-rate
// (发芽率) computation: the latest day in the locked schedule.
func FinalDay(dayAges []int32) DayAge {
	if len(dayAges) == 0 {
		return 0
	}
	max := dayAges[0]
	for _, d := range dayAges[1:] {
		if d > max {
			max = d
		}
	}
	return DayAge(max)
}

// NormalCount sums the normal seedlings observed for a blind code at a
// specific day age across valid cells.
func NormalCount(cells []GerminationCell, code blindcode.BlindCode, day DayAge) int {
	total := 0
	for _, c := range cells {
		if c.Valid && c.BlindCode == code && c.DayAge == day {
			total += c.Normal
		}
	}
	return total
}

// Percent computes a fixed-point percentage in basis points (10^4 scale)
// using deterministic integer arithmetic: value * 10000 / total. It rejects
// a zero or negative total with CodeBadRequest and clamps a negative value to
// zero before dividing.
func Percent(value, total int) (int32, *domain.Error) {
	if total <= 0 {
		return 0, domain.NewError(domain.CodeBadRequest, "non-positive grain total")
	}
	if value < 0 {
		value = 0
	}
	return int32(value * 10000 / total), nil
}

// Energy derives the germination-energy percentage (basis points) for a blind
// code at the energy observation day.
func Energy(cells []GerminationCell, code blindcode.BlindCode, dayAges []int32, grains int) (int32, *domain.Error) {
	normal := NormalCount(cells, code, EnergyDay(dayAges))
	return Percent(normal, grains)
}

// Rate derives the germination-rate percentage (basis points) for a blind
// code at the final observation day.
func Rate(cells []GerminationCell, code blindcode.BlindCode, dayAges []int32, grains int) (int32, *domain.Error) {
	normal := NormalCount(cells, code, FinalDay(dayAges))
	return Percent(normal, grains)
}
