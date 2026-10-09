package rest

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/container"
	"gofr.dev/pkg/gofr/logging"
	"gofr.dev/pkg/gofr/testutil"

	"github.com/dennis-dko/go-time-recording/internal/support/spreadsheet"
)

// An upload that made the reader panic is refused as a file that cannot be read,
// and the log says where the reader gave way.
//
// The guard turns the dependency's panic into that refusal, which is right for
// the person and was the end of it for everybody else: no stack reached the log,
// so a bug in the parser looked like a file somebody got wrong, and nobody could
// report it upstream.
func TestAReaderThatPanickedIsLoggedWithWhereItGaveWay(t *testing.T) {
	caught := fmt.Errorf("%w: %w", spreadsheet.ErrUnreadableWorkbook, &spreadsheet.Panic{
		Value: "runtime error: index out of range [-1]",
		Stack: []byte("goroutine 7 [running]:\ngithub.com/xuri/excelize/v2.(*File).getFromStringItem(...)"),
	})

	var answered error

	logged := testutil.StderrOutputForFunc(func() {
		ctx := &gofr.Context{
			Context:   context.Background(),
			Container: &container.Container{Logger: logging.NewMockLogger(logging.INFO)},
		}

		answered = unreadableWorkbook(ctx, caught)
	})

	var coded interface{ Response() map[string]any }

	if !errors.As(answered, &coded) || coded.Response()["code"] != "notAWorkbook" {
		t.Errorf("the upload was answered %v, want the refusal of a file that cannot be read", answered)
	}

	if !strings.Contains(logged, "getFromStringItem") {
		t.Errorf("the log does not say where the reader gave way:\n%s", logged)
	}
}
