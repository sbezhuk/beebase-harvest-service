package harvest

// Product is the kind of beekeeping product a Harvest record refers to -
// a closed set. Adding a new value (or an OTHER/custom product) is a
// deliberate out-of-scope decision for this stage of the project, not
// something a client can send freely.
type Product string

// Values are UPPER_SNAKE_CASE, this project's convention for enum wire
// values.
const (
	ProductHoney    Product = "HONEY"
	ProductPollen   Product = "POLLEN"
	ProductPropolis Product = "PROPOLIS"
	ProductWax      Product = "WAX"
)

// Valid reports whether p is one of the known products.
func (p Product) Valid() bool {
	switch p {
	case ProductHoney, ProductPollen, ProductPropolis, ProductWax:
		return true
	default:
		return false
	}
}
