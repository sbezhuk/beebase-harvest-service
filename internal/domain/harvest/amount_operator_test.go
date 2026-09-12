package harvest

import "testing"

func TestAmountOperator_Valid(t *testing.T) {
	for _, o := range []AmountOperator{AmountOperatorGT, AmountOperatorLT, AmountOperatorEQ} {
		if !o.Valid() {
			t.Errorf("%q.Valid() = false, want true", o)
		}
	}
	if AmountOperator("gte").Valid() {
		t.Error(`AmountOperator("gte").Valid() = true, want false`)
	}
	if AmountOperator("").Valid() {
		t.Error(`AmountOperator("").Valid() = true, want false`)
	}
}

func TestAmountOperator_SQL(t *testing.T) {
	tests := map[AmountOperator]string{
		AmountOperatorGT: ">",
		AmountOperatorLT: "<",
		AmountOperatorEQ: "=",
	}
	for op, want := range tests {
		if got := op.SQL(); got != want {
			t.Errorf("%q.SQL() = %q, want %q", op, got, want)
		}
	}
	if got := AmountOperator("gte").SQL(); got != "" {
		t.Errorf(`AmountOperator("gte").SQL() = %q, want ""`, got)
	}
}
