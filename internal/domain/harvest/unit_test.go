package harvest

import "testing"

func TestProduct_Valid(t *testing.T) {
	for _, p := range []Product{ProductHoney, ProductPollen, ProductPropolis, ProductWax} {
		if !p.Valid() {
			t.Errorf("%q.Valid() = false, want true", p)
		}
	}
	if Product("OTHER").Valid() {
		t.Error(`Product("OTHER").Valid() = true, want false`)
	}
	if Product("").Valid() {
		t.Error(`Product("").Valid() = true, want false`)
	}
}

func TestUnit_Valid(t *testing.T) {
	for _, u := range []Unit{UnitGram, UnitKilogram, UnitLiter} {
		if !u.Valid() {
			t.Errorf("%q.Valid() = false, want true", u)
		}
	}
	if Unit("ml").Valid() {
		t.Error(`Unit("ml").Valid() = true, want false`)
	}
}

func TestValidCombination(t *testing.T) {
	valid := []struct {
		product Product
		unit    Unit
	}{
		{ProductHoney, UnitKilogram},
		{ProductHoney, UnitLiter},
		{ProductPollen, UnitGram},
		{ProductPollen, UnitKilogram},
		{ProductPropolis, UnitGram},
		{ProductWax, UnitGram},
	}
	for _, tc := range valid {
		if !ValidCombination(tc.product, tc.unit) {
			t.Errorf("ValidCombination(%s, %s) = false, want true", tc.product, tc.unit)
		}
	}

	invalid := []struct {
		product Product
		unit    Unit
	}{
		{ProductHoney, UnitGram},
		{ProductPollen, UnitLiter},
		{ProductPropolis, UnitKilogram},
		{ProductPropolis, UnitLiter},
		{ProductWax, UnitKilogram},
		{ProductWax, UnitLiter},
	}
	for _, tc := range invalid {
		if ValidCombination(tc.product, tc.unit) {
			t.Errorf("ValidCombination(%s, %s) = true, want false", tc.product, tc.unit)
		}
	}
}
