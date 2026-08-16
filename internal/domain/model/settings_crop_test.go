package model

import "testing"

// A crop survives being written down and read back.
//
// It goes into the settings table as four numbers in a string, because that table
// is read by people when something has gone wrong and an escaped object is not.
// The round trip is the whole contract.
func TestACropSurvivesStorage(t *testing.T) {
	crop := LogoCrop{X: 0.125, Y: 0, W: 0.2, H: 1}

	if crop.Whole() {
		t.Fatal("a fifth of the image counts as the whole of it")
	}

	back := ParseLogoCrop(crop.String())

	if back != crop {
		t.Errorf("stored as %q and came back as %+v", crop.String(), back)
	}
}

// The whole image is stored as nothing, which is what most installations have.
func TestTheWholeImageIsStoredAsNothing(t *testing.T) {
	if got := (LogoCrop{}).String(); got != "" {
		t.Errorf("the whole image is written down as %q", got)
	}

	if !ParseLogoCrop("").Whole() {
		t.Error("an empty setting is not read as the whole image")
	}
}

// Anything unreadable is the whole image: a logo shown complete is never wrong,
// only sometimes small, and nothing here should be able to produce an empty tab.
func TestAnUnreadableCropIsTheWholeImage(t *testing.T) {
	for _, raw := range []string{"garbage", "1,2", "a,b,c,d", "0.1,0.2,0.3"} {
		if !ParseLogoCrop(raw).Whole() {
			t.Errorf("%q was read as a selection", raw)
		}
	}
}
