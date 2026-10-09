package spreadsheet

import (
	"errors"
	"strings"
	"testing"
)

// A panic in the reader is answered as an unreadable workbook, and keeps what the
// panic was and where it happened.
//
// The guard kept the panic's value in the sentence and nothing else, so the one
// thing that tells a dependency's bug apart - the stack - was gone by the time
// anybody read the log, and with no stack nobody reports it upstream. The person
// still gets a refusal they can act on; the stack now goes with it, for the log.
func TestAPanicInTheReaderKeepsWhereItHappened(t *testing.T) {
	rows, err := guarded(func() ([][]string, error) {
		parserGivesWay()

		return [][]string{{"never reached"}}, nil
	})

	if rows != nil || !errors.Is(err, ErrUnreadableWorkbook) {
		t.Fatalf("a panic in the reader came back as %d rows and %v, want no rows and an "+
			"unreadable workbook", len(rows), err)
	}

	caught, ok := errors.AsType[*Panic](err)
	if !ok {
		t.Fatalf("the refusal carries no panic to log: %v", err)
	}

	if caught.Value != "the parser gave way" {
		t.Errorf("the panic is kept as %v", caught.Value)
	}

	if !strings.Contains(string(caught.Stack), "parserGivesWay") {
		t.Errorf("the stack kept does not say where the parser gave way:\n%s", caught.Stack)
	}
}

func parserGivesWay() { panic("the parser gave way") }
