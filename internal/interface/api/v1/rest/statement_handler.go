package rest

import (
	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/http/response"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/common"
	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/support/spreadsheet"
)

// StatementHandler serves the statement of work: what somebody's billable hours
// come to over a period, project by project - on screen, and as a workbook.
type StatementHandler struct {
	statements *service.StatementService
	authz      *Authorizer
	timezone   InstanceTimezoneFunc
}

// NewStatementHandler creates the handler.
func NewStatementHandler(statements *service.StatementService, authz *Authorizer,
	timezone InstanceTimezoneFunc) *StatementHandler {
	return &StatementHandler{statements: statements, authz: authz, timezone: timezone}
}

// StatementLineResponse is one billable project. HourlyRate and Amount are in
// whole units of Currency and null where the project has no rate yet.
type StatementLineResponse struct {
	ProjectID  uint     `json:"projectId"`
	Name       string   `json:"name"`
	Hours      float64  `json:"hours"`
	Currency   string   `json:"currency"`
	HourlyRate *float64 `json:"hourlyRate"`
	Amount     *float64 `json:"amount"`
}

// StatementTotalResponse is what the priced lines in one currency come to.
type StatementTotalResponse struct {
	Currency string  `json:"currency"`
	Hours    float64 `json:"hours"`
	Amount   float64 `json:"amount"`
}

// StatementResponse is a statement of work on the wire.
type StatementResponse struct {
	From   Date                     `json:"from"`
	To     Date                     `json:"to"`
	Lines  []StatementLineResponse  `json:"lines"`
	Totals []StatementTotalResponse `json:"totals"`
}

// Statement handles GET /api/v1/reports/statement: the caller's own, or with
// userId that of an account the caller looks after as a manager.
func (h *StatementHandler) Statement(c *gofr.Context) (any, error) {
	statement, err := h.read(c)
	if err != nil {
		return nil, err
	}

	answer := StatementResponse{
		From: Date{Time: statement.From}, To: Date{Time: statement.To},
		Lines:  make([]StatementLineResponse, 0, len(statement.Lines)),
		Totals: make([]StatementTotalResponse, 0, len(statement.Totals)),
	}

	for _, line := range statement.Lines {
		answer.Lines = append(answer.Lines, StatementLineResponse{
			ProjectID: line.ProjectID, Name: line.Name, Hours: line.Hours, Currency: line.Currency,
			HourlyRate: common.UnitsOf(line.HourlyRate), Amount: common.UnitsOf(line.Amount),
		})
	}

	for _, total := range statement.Totals {
		answer.Totals = append(answer.Totals, StatementTotalResponse{
			Currency: total.Currency, Hours: total.Hours, Amount: float64(total.Amount) / 100,
		})
	}

	return answer, nil
}

// Export handles GET /api/v1/reports/statement/export: the same statement as a
// workbook, its words in the reader's language, a total per currency under the
// lines.
func (h *StatementHandler) Export(c *gofr.Context) (any, error) {
	statement, err := h.read(c)
	if err != nil {
		return nil, err
	}

	rows := make([]spreadsheet.StatementRow, 0, len(statement.Lines)+len(statement.Totals))

	for _, line := range statement.Lines {
		rows = append(rows, spreadsheet.StatementRow{
			Project: line.Name, Hours: line.Hours, Currency: line.Currency,
			Rate: common.UnitsOf(line.HourlyRate), Amount: common.UnitsOf(line.Amount),
		})
	}

	for _, total := range statement.Totals {
		amount := float64(total.Amount) / 100

		rows = append(rows, spreadsheet.StatementRow{
			Total: true, Hours: total.Hours, Currency: total.Currency, Amount: &amount,
		})
	}

	book, err := spreadsheet.WriteStatement(language(c), rows)
	if err != nil {
		return nil, toHTTPError(err)
	}

	return response.File{Content: book, ContentType: xlsxContentType}, nil
}

// read works out the statement a request asks for, over the period it names - the
// month so far, in the zone of the account it is about, where it names none.
func (h *StatementHandler) read(c *gofr.Context) (*service.Statement, error) {
	principal, err := h.authz.Require(c, model.PermReportReadOwn)
	if err != nil {
		return nil, err
	}

	requested, err := queryUint(c, "userId")
	if err != nil {
		return nil, toHTTPError(err)
	}

	account, err := h.authz.accountFor(c, principal, requested, somebodyElsesEntries(),
		model.PermTimesheetReadTeam)
	if err != nil {
		return nil, err
	}

	from, to, err := overtimeRange(c, account.TimezoneOf(h.timezone.resolve(c)))
	if err != nil {
		return nil, toHTTPError(err)
	}

	statement, err := h.statements.Statement(c, account.ID, from, to)
	if err != nil {
		return nil, toHTTPError(err)
	}

	return statement, nil
}
