package harvest

// Unit is the unit an amount was recorded in. Wire values are lowercase
// per this resource's own spec, unlike Product's UPPER_SNAKE_CASE.
type Unit string

const (
	UnitGram     Unit = "g"
	UnitKilogram Unit = "kg"
	UnitLiter    Unit = "l"
)

// Valid reports whether u is one of the known units.
func (u Unit) Valid() bool {
	switch u {
	case UnitGram, UnitKilogram, UnitLiter:
		return true
	default:
		return false
	}
}

// allowedUnits is the single source of truth for which units each product
// may be recorded in: no automatic conversion between units (e.g. Honey
// between kg and l) is ever performed, so a product simply can't be
// combined with a unit that isn't listed here.
var allowedUnits = map[Product]map[Unit]bool{
	ProductHoney:    {UnitKilogram: true, UnitLiter: true},
	ProductPollen:   {UnitGram: true, UnitKilogram: true},
	ProductPropolis: {UnitGram: true},
	ProductWax:      {UnitGram: true},
}

// ValidCombination reports whether unit is an allowed unit for product.
// An invalid product or unit is simply never present in allowedUnits, so
// it's rejected here too - callers should still validate Product.Valid()
// and Unit.Valid() independently for a more specific error.
func ValidCombination(product Product, unit Unit) bool {
	return allowedUnits[product][unit]
}
