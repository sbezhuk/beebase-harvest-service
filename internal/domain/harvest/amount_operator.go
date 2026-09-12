package harvest

// AmountOperator is the comparison applied by the harvest list endpoint's
// amount filter. Wire values are lowercase, matching Unit's own
// convention for this resource.
type AmountOperator string

const (
	AmountOperatorGT AmountOperator = "gt"
	AmountOperatorLT AmountOperator = "lt"
	AmountOperatorEQ AmountOperator = "eq"
)

// Valid reports whether o is one of the known operators.
func (o AmountOperator) Valid() bool {
	switch o {
	case AmountOperatorGT, AmountOperatorLT, AmountOperatorEQ:
		return true
	default:
		return false
	}
}

// SQL returns the SQL comparison operator o represents. Callers must
// have already checked Valid() - an unrecognized AmountOperator returns
// "", which is not valid SQL and would fail loudly rather than silently
// building the wrong comparison.
func (o AmountOperator) SQL() string {
	switch o {
	case AmountOperatorGT:
		return ">"
	case AmountOperatorLT:
		return "<"
	case AmountOperatorEQ:
		return "="
	default:
		return ""
	}
}
