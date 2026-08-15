package rest

import (
	"errors"
	"strings"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
)

// Something that failed inside says so twice: once in a sentence anybody can
// read, and once in the words of whatever actually failed.
//
// The wording of a failure comes from wherever it happened - a driver, a file
// system, a directory library - and it is always English, always somebody else's,
// and drawn from a set with no end. It cannot be translated, so it used to be the
// message: a German reader got "dial tcp 10.0.0.4:5432: connect: connection
// refused" and no sentence at all.
func TestAnInternalFailureAnswersGenericallyAndKeepsTheOriginal(t *testing.T) {
	underneath := errors.New("dial tcp 10.0.0.4:5432: connect: connection refused")

	answer := newInternalError(apperror.Internal(underneath))

	body := answer.Response()

	if got := body["code"]; got != apperror.CodeInternal {
		t.Errorf("the answer carries code %v, want %q", got, apperror.CodeInternal)
	}

	// The generic sentence is what a reader is shown, and it must not be the
	// driver's - which is the whole point of the exercise.
	if strings.Contains(answer.message, "dial tcp") {
		t.Errorf("the message shown is still the driver's: %q", answer.message)
	}

	// And the driver's own words survive, because they are the only text that
	// says what happened.
	detail, ok := body["detail"].(string)
	if !ok || !strings.Contains(detail, "connection refused") {
		t.Errorf("the original wording did not survive as detail: %v", body["detail"])
	}

	if answer.StatusCode() != 500 {
		t.Errorf("an internal failure answers %d", answer.StatusCode())
	}
}

// The reference is on the screen and in the log, and it is the same one.
//
// This is what a generic message costs if nobody pays it: somebody reports "it
// says the request could not be completed", and there is no way to tell which of
// the day's failures they mean. Error() is what GoFr writes to the log, so the
// reference goes in there as well as into the body.
func TestTheReferenceOnScreenIsTheOneInTheLog(t *testing.T) {
	answer := newInternalError(apperror.Internal(errors.New("permission denied")))

	ref, ok := answer.Response()["ref"].(string)
	if !ok || ref == "" {
		t.Fatal("the answer carries no reference")
	}

	if !strings.Contains(answer.Error(), ref) {
		t.Errorf("the log line %q does not carry the reference %q shown on screen",
			answer.Error(), ref)
	}

	// The log gets the original wording too. It is the one place where being
	// specific costs nothing and being generic costs everything.
	if !strings.Contains(answer.Error(), "permission denied") {
		t.Errorf("the log line %q does not say what actually failed", answer.Error())
	}
}

// Two failures are told apart.
func TestEachFailureGetsItsOwnReference(t *testing.T) {
	first := newInternalError(apperror.Internal(errors.New("one")))
	second := newInternalError(apperror.Internal(errors.New("two")))

	if first.ref == second.ref {
		t.Errorf("two failures share the reference %q, so a report naming one "+
			"names both", first.ref)
	}

	// Readable over a telephone: no vowels to spell words, and nothing that is
	// read as something else.
	for _, char := range first.ref {
		if !strings.ContainsRune(referenceAlphabet, char) {
			t.Errorf("the reference %q contains %q, which is not in the alphabet "+
				"chosen for being read aloud", first.ref, char)
		}
	}
}

// A rule that named itself keeps its own code rather than being flattened.
//
// The generic code is for what could not be anticipated. A failure that was
// anticipated well enough to be given a name has a sentence waiting for it, and
// replacing that with "something went wrong" would be a step backwards.
func TestAnInternalFailureThatNamesItselfKeepsItsName(t *testing.T) {
	named := apperror.Internal(errors.New("the exporter refused")).
		WithCode("exporterUnavailable")

	if got := newInternalError(named).Response()["code"]; got != "exporterUnavailable" {
		t.Errorf("the code is %v rather than the one the failure gave itself", got)
	}
}

// A connection test says the same thing the same way.
//
// It answers 200 with the reason inside it - a database that cannot be reached is
// information about what somebody typed rather than a fault here - which is
// exactly how it ended up outside the path every other refusal takes, showing the
// driver's English on the one screen where the values being complained about are
// sitting in the fields above it.
func TestAFailedConnectionTestIsGenericWithTheDriversWordsKept(t *testing.T) {
	out := probeFailure(errors.New(`pq: password authentication failed for user "gtr"`))

	if got := out["code"]; got != apperror.CodeProbeFailed {
		t.Errorf("a failed probe carries code %v, want %q", got, apperror.CodeProbeFailed)
	}

	if message, _ := out["message"].(string); strings.Contains(message, "pq:") {
		t.Errorf("the sentence shown is the driver's: %q", message)
	}

	if detail, _ := out["detail"].(string); !strings.Contains(detail, "password authentication") {
		t.Errorf("the driver's wording was lost: %v", out["detail"])
	}
}

// A complaint this application made about a field keeps its own shape.
//
// Those already translate - the field names travel as data and the interface
// names them the way the labels above them do - so sweeping them into the generic
// sentence would take a specific, useful refusal and make it vague.
func TestAFieldComplaintIsNotMadeGeneric(t *testing.T) {
	out := probeFailure(apperror.InvalidFields("host", "user"))

	if got := out["code"]; got == apperror.CodeProbeFailed {
		t.Error("a complaint about empty fields was flattened into the generic one")
	}

	fields, ok := out["param"].([]string)
	if !ok || len(fields) != 2 {
		t.Errorf("the field names did not survive: %v", out["param"])
	}
}
