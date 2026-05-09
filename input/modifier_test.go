package input

import "testing"

func TestModifier_DistinctValues(t *testing.T) {
	mods := []Modifier{ModCtrl, ModAlt, ModShift, ModWin}
	for i, a := range mods {
		for j, b := range mods {
			if i == j {
				continue
			}
			if a == b {
				t.Errorf("modifier collision: mods[%d] == mods[%d] = %d", i, j, a)
			}
		}
	}
}

func TestModifier_BitwiseOrComposable(t *testing.T) {
	combo := ModCtrl | ModShift
	if combo&ModCtrl == 0 {
		t.Error("combo missing ModCtrl bit")
	}
	if combo&ModShift == 0 {
		t.Error("combo missing ModShift bit")
	}
	if combo&ModAlt != 0 {
		t.Error("combo unexpectedly has ModAlt bit")
	}
}
