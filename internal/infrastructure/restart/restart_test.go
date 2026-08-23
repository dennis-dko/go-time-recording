package restart

import "testing"

// What the button does is reported, and it is one of the two things it can be.
//
// Not asserted as a fixed value: whether this runs in a container is a fact
// about the machine the suite is on, and it is not the same fact on all of them
// - which is the point. What has to hold everywhere is that the screen is told
// something it can render, because an empty mode renders as no sentence at all
// under a button whose behaviour differs by where it is pressed.
//
// No build tag, so this runs on Windows too, where the whole thing is refused
// and the mode still has to be one the screen knows.
func TestTheModeIsAlwaysOneOfTheTwo(t *testing.T) {
	switch got := Mode(); got {
	case ModeProcess, ModeContainer:
	default:
		t.Errorf("the mode is %q, which the screen has no sentence for", got)
	}
}

// A container can always restart, whatever it knows about its own binary.
//
// Supported used to answer that question with os.Executable alone, which is the
// right question for replacing this process and the wrong one for stopping it.
// Inside a container the button works without the binary being locatable at all,
// because what starts the next one is not this process.
func TestRestartingIsOfferedWhereverOneOfTheTwoWorks(t *testing.T) {
	if Mode() == ModeContainer && !Supported() {
		t.Error("a container is told it cannot restart, when stopping is all it takes")
	}

	if !Supported() && Code() == "" {
		t.Error("a refusal names no reason, so the screen has nothing to explain")
	}

	if Supported() && (Code() != "" || Why() != "") {
		t.Errorf("a working restart carries a refusal: code %q, reason %q", Code(), Why())
	}
}
