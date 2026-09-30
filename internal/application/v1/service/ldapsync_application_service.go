package service

import (
	"context"
	"math"
	"sort"
	"sync"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// DirectoryLister reads the accounts a directory holds. It is an interface so
// this service does not depend on the LDAP client and can be tested without a
// directory.
type DirectoryLister interface {
	Enabled() bool
	ListUsers(ctx context.Context) ([]ExternalUser, error)
}

// SyncCandidate is one local account the directory no longer knows.
type SyncCandidate struct {
	UserID uint
	Name   string
	Email  string

	// Timesheets is how many recorded entries would be destroyed with the
	// account. It is reported so nobody deletes a year of work unaware.
	Timesheets int
}

// SyncReport is the outcome of a synchronisation run, or of a preview.
type SyncReport struct {
	// DirectoryUsers is how many accounts the directory returned.
	DirectoryUsers int

	// LocalExternal is how many local accounts came from the directory.
	LocalExternal int

	// Candidates are the accounts missing upstream.
	Candidates []SyncCandidate

	// Deleted is what was actually removed; empty for a preview.
	Deleted []SyncCandidate

	// Created lists accounts added because the directory has them and this
	// installation did not.
	Created []string

	// Aborted explains why nothing was deleted, when a guard stopped the run:
	// the sentence, for a log. AbortCode and AbortValues say the same for a
	// screen, which puts it in the reader's language as it does any refusal -
	// the sentence alone reached a German administrator in English.
	Aborted     string
	AbortCode   string
	AbortValues []any

	// DryRun reports whether this was a preview.
	DryRun bool
}

// LDAPSyncService reconciles the local accounts with the directory.
//
// The directory is only ever read. Everything this service changes happens in
// the application's own database.
type LDAPSyncService struct {
	directory  DirectoryLister
	users      repository.UserRepository
	roles      repository.RoleRepository
	timesheets repository.TimesheetRepository
	purger     UserPurger

	// maxDeleteRatio caps how much of the external population one run may
	// remove. A directory that answers with a truncated list would otherwise
	// read as a mass departure.
	maxDeleteRatio float64

	// limits, when attached, supplies the administered ratio instead, so the
	// safety net can be adjusted without a restart.
	limits *LimitsProvider

	defaultRole string

	// running is held for the whole of a run that changes anything, and taken
	// rather than waited on.
	//
	// The schedule and the button share this service, and two administrators can
	// press the button at once. Two runs read the same directory and the same
	// accounts, so they choose the same departures, and the second to reach one
	// found it already purged, was refused with "user not found" and stopped
	// part-way, having deleted whatever it reached first. Nothing was lost - each
	// purge is its own transaction - but the run that stopped reported a failure
	// about an account it never touched. A preview changes nothing and does not
	// take it.
	running sync.Mutex

	metrics
}

// WithMetrics attaches the recorder. Optional: without it the synchronisation
// works and records nothing.
func (s *LDAPSyncService) WithMetrics(recorder Recorder) *LDAPSyncService {
	s.recorder = recorder

	return s
}

// UserPurger removes a user together with everything referencing them.
type UserPurger interface {
	PurgeUser(ctx context.Context, userID uint) error
}

// NewLDAPSyncService takes two settings beside its repositories. maxDeleteRatio
// is the share of the directory's accounts one run may remove, which WithLimits
// lets the Settings screen move without a restart; defaultRole is the role an
// account the directory brings in starts with when the directory settings name
// none that may be given - see roleForArrival - and the ordinary one when empty.
func NewLDAPSyncService(
	directory DirectoryLister,
	users repository.UserRepository,
	roles repository.RoleRepository,
	timesheets repository.TimesheetRepository,
	purger UserPurger,
	maxDeleteRatio float64,
	defaultRole string,
) *LDAPSyncService {
	if defaultRole == "" {
		defaultRole = model.RoleUser
	}

	return &LDAPSyncService{
		directory:      directory,
		users:          users,
		roles:          roles,
		timesheets:     timesheets,
		purger:         purger,
		maxDeleteRatio: maxDeleteRatio,
		defaultRole:    defaultRole,
	}
}

// Preview reports what a synchronisation would change, without changing it.
func (s *LDAPSyncService) Preview(ctx context.Context) (*SyncReport, error) {
	return s.run(ctx, true, nil)
}

// Sync reconciles the local accounts with the directory.
//
// Accounts the directory no longer holds are removed together with their time
// entries, private projects, tokens and sessions. Accounts the directory has
// and this installation does not are created.
func (s *LDAPSyncService) Sync(ctx context.Context) (*SyncReport, error) {
	return s.run(ctx, false, nil)
}

// SyncAsConfirmed is Sync bound to what somebody agreed to: it deletes exactly
// the accounts named, or nothing at all.
//
// A confirmation is given against a preview, and the run asks the directory
// again, so whatever the directory answers in between is what would be deleted.
// Unbound, an answer that had moved took accounts nobody was shown, and a
// preview proposing nobody asked nothing while the run was free to remove up to
// the deletion limit. A run whose candidates are not the confirmed ones is
// refused with them in the report, so the screen can show them and ask again,
// and it creates nothing either: the answer is not the one that was looked at.
//
// Sync stays unbound for the scheduled run, which has nobody to ask and is held
// by the deletion limit alone.
func (s *LDAPSyncService) SyncAsConfirmed(ctx context.Context, confirmed []uint) (*SyncReport, error) {
	agreed := make(map[uint]bool, len(confirmed))
	for _, id := range confirmed {
		agreed[id] = true
	}

	return s.run(ctx, false, func(report *SyncReport) *apperror.Error {
		same := len(report.Candidates) == len(agreed)

		for _, candidate := range report.Candidates {
			same = same && agreed[candidate.UserID]
		}

		if same {
			return nil
		}

		return apperror.Conflictf("the directory now answers differently from the preview "+
			"that was confirmed: this run would delete %d account(s), so nothing was changed; "+
			"preview again", len(report.Candidates)).
			WithCode("syncDiffersFromPreview", len(report.Candidates))
	})
}

// run is the body Sync, SyncAsConfirmed and Preview share; dryRun decides
// whether the deletions are carried out or only reported, and approve, when
// given, is a last guard that may refuse the run once its candidates are known.
//
//nolint:cyclop // the guards are the point of this function; splitting them across helpers would scatter the reasons a destructive run is refused
func (s *LDAPSyncService) run(
	ctx context.Context,
	dryRun bool,
	approve func(*SyncReport) *apperror.Error,
) (*SyncReport, error) {
	if !s.directory.Enabled() {
		return nil, apperror.Conflictf("no directory is configured").WithCode("noDirectory")
	}

	if !dryRun {
		if !s.running.TryLock() {
			return nil, apperror.Conflictf("a directory synchronisation is already running").
				WithCode("syncAlreadyRunning")
		}

		defer s.running.Unlock()
	}

	directoryUsers, err := s.directory.ListUsers(ctx)
	if err != nil {
		// A failed read must never be treated as "the directory is empty".
		return nil, apperror.Internal(err)
	}

	report := &SyncReport{DirectoryUsers: len(directoryUsers), DryRun: dryRun}

	// Two indexes: the stable identifier is authoritative, the mail address
	// only covers accounts that predate identifiers being recorded.
	byID := make(map[string]ExternalUser, len(directoryUsers))
	byEmail := make(map[string]ExternalUser, len(directoryUsers))

	for _, u := range directoryUsers {
		if u.ID != "" {
			byID[u.ID] = u
		}

		byEmail[normalizeEmail(u.Email)] = u
	}

	localUsers, err := s.users.GetAll(ctx)
	if err != nil {
		return nil, err
	}

	knownIDs := make(map[string]bool, len(localUsers))
	knownEmails := make(map[string]bool, len(localUsers))

	for _, user := range localUsers {
		knownEmails[normalizeEmail(user.Email)] = true

		if user.ExternalID != "" {
			knownIDs[user.ExternalID] = true
		}

		// Only directory-backed accounts are in scope. A local account was
		// never in the directory, so its absence there means nothing.
		if !user.IsExternal {
			continue
		}

		// The built-in administrator is never removed: it is the guaranteed
		// way back into an installation.
		if user.IsSystem {
			continue
		}

		report.LocalExternal++

		if s.stillInDirectory(user, byID, byEmail) {
			continue
		}

		count, countErr := s.countTimesheets(ctx, user.ID)
		if countErr != nil {
			return nil, countErr
		}

		report.Candidates = append(report.Candidates, SyncCandidate{
			UserID: user.ID, Name: user.Name, Email: user.Email, Timesheets: count,
		})
	}

	sort.Slice(report.Candidates, func(i, j int) bool {
		return report.Candidates[i].Email < report.Candidates[j].Email
	})

	// An empty directory answer is almost always a broken filter, a wrong
	// base DN or an outage - not everybody leaving at once.
	if len(directoryUsers) == 0 {
		report.abort(apperror.Conflictf(
			"the directory returned no users at all; refusing to delete anyone").
			WithCode("syncDirectoryAnsweredEmpty"))

		return report, nil
	}

	if reason := s.exceedsRatio(ctx, report); reason != nil {
		report.abort(reason)

		return report, nil
	}

	if approve != nil {
		if reason := approve(report); reason != nil {
			report.abort(reason)

			return report, nil
		}
	}

	// The report goes back with the error here too: createMissing may have
	// written accounts before it failed, and what it wrote is in the report.
	if err := s.createMissing(ctx, directoryUsers, knownIDs, knownEmails, report, dryRun); err != nil {
		return report, stoppedPartWay(report, err)
	}

	if dryRun {
		return report, nil
	}

	for _, candidate := range report.Candidates {
		if err := s.purger.PurgeUser(ctx, candidate.UserID); err != nil {
			// The report goes back with the error, and that is the point rather
			// than tidiness. Each purge is its own transaction, so the ones before
			// this are done and cannot be undone - and the only record of an
			// irreversible deletion is the line a caller writes from
			// report.Deleted. Returning nil here left a run that removed three
			// accounts and then lost the database saying "directory sync failed"
			// and nothing else: three people's hours gone, and nothing naming
			// them.
			return report, stoppedPartWay(report, err)
		}

		report.Deleted = append(report.Deleted, candidate)

		// Counted one at a time, and only after the deletion actually happened.
		// This is the one operation here that removes people together with the
		// hours they recorded, so the number that matters is what was done, not
		// what was planned.
		s.count(ctx, MetricDirectoryAccounts, "action", "deleted")
	}

	return report, nil
}

// stoppedPartWay is the error a run ends with once it has changed something: the
// cause, counted with how many accounts it had already deleted and created.
//
// The report names them and the caller logs it, but a screen that started the
// run was answered with the error alone, and went on showing the preview - a
// number of accounts that "would be deleted", some of them already gone, under a
// sentence saying the run had failed. The counts travel with the error so the
// screen can say what did happen, and the cause stays the original for the log.
// A run that changed nothing ends with its cause as it was.
func stoppedPartWay(report *SyncReport, cause error) error {
	if len(report.Deleted) == 0 && len(report.Created) == 0 {
		return cause
	}

	return apperror.Internal(cause).
		WithCode("syncStoppedPartWay", len(report.Deleted), len(report.Created))
}

// stillInDirectory reports whether the directory still holds this account.
//
// The stable identifier decides whenever the account has one: a renamed
// mailbox then keeps matching, instead of looking like a departure that would
// take the person's recorded hours with it. Only an account with no identifier
// yet - one created before identifiers were recorded - falls back to the mail
// address.
//
// So does an entry that arrives with no identifier, which is every entry once
// the identifier attribute is cleared. An entry with an identifier of its own
// under the account's address is a different entry, a successor who inherited
// the mailbox; one with none says nothing of the kind, and reading its silence
// as a departure deleted people the directory still held.
func (s *LDAPSyncService) stillInDirectory(
	user *model.User,
	byID map[string]ExternalUser,
	byEmail map[string]ExternalUser,
) bool {
	if user.ExternalID != "" {
		if _, found := byID[user.ExternalID]; found {
			return true
		}
	}

	entry, found := byEmail[normalizeEmail(user.Email)]

	return found && (user.ExternalID == "" || entry.ID == "")
}

// abort records why a guard stopped the run, once for a log and once for a screen.
func (r *SyncReport) abort(reason *apperror.Error) {
	r.Aborted, r.AbortCode, r.AbortValues = reason.Error(), reason.Code, reason.Values
}

// exceedsRatio reports why the run is refused when it would remove more of the
// directory-backed population than the configured share, or nil when it would not.
func (s *LDAPSyncService) exceedsRatio(ctx context.Context, report *SyncReport) *apperror.Error {
	ratioLimit := s.deleteRatio(ctx)
	if ratioLimit <= 0 || report.LocalExternal == 0 || len(report.Candidates) == 0 {
		return nil
	}

	ratio := float64(len(report.Candidates)) / float64(report.LocalExternal)
	if ratio <= ratioLimit {
		return nil
	}

	removing, of := len(report.Candidates), report.LocalExternal
	share, limit := int(math.Round(ratio*100)), int(math.Round(ratioLimit*100))

	return apperror.Conflictf(
		"would remove %d of %d directory accounts (%d%%), above the %d%% safety limit; "+
			"check the directory filter and base DN, then raise the deletion limit under "+
			"Operation and limits, or LDAP_SYNC_MAX_DELETE_RATIO, if this really is intended",
		removing, of, share, limit).
		WithCode("syncWouldRemoveTooMany", removing, of, share, limit)
}

// createMissing adds accounts the directory holds and this installation does
// not, so people can be prepared before their first sign-in.
func (s *LDAPSyncService) createMissing(
	ctx context.Context,
	directoryUsers []ExternalUser,
	knownIDs map[string]bool,
	knownEmails map[string]bool,
	report *SyncReport,
	dryRun bool,
) error {
	// Resolved once per role the answer names rather than once per entry: every
	// entry of one answer carries the same default role, read from the settings
	// the client held when it listed them.
	arrivals := map[string]*model.Role{}

	for _, directoryUser := range directoryUsers {
		email := normalizeEmail(directoryUser.Email)
		if email == "" {
			continue
		}

		// Known by either key: an account whose address changed upstream is
		// already matched by its identifier and must not be created twice.
		if knownEmails[email] || (directoryUser.ID != "" && knownIDs[directoryUser.ID]) {
			continue
		}

		// Known from here on, including to this loop.
		//
		// The two maps are built once from the local accounts, and nothing told
		// them about the accounts this run had just made. A directory answer
		// holding the same person twice - a search matching an entry in two
		// organisational units, a filter joining a group membership - therefore
		// reached the save twice, and the second was refused as a duplicate
		// address. That error aborted the whole run, after some accounts had been
		// created and before any deletion had been made, and it reported "a user
		// with that email already exists", which reads as a problem with the
		// address rather than with the answer.
		knownEmails[email] = true

		if directoryUser.ID != "" {
			knownIDs[directoryUser.ID] = true
		}

		if dryRun {
			report.Created = append(report.Created, email)

			continue
		}

		role, resolved := arrivals[directoryUser.Role]
		if !resolved {
			var err error

			role, err = roleForArrival(ctx, s.roles, directoryUser.Role, s.defaultRole)
			if err != nil {
				return err
			}

			arrivals[directoryUser.Role] = role
		}

		name := directoryUser.Name
		if name == "" {
			name = email
		}

		// No working times. Zero is how "follow the default" is stored, which is what
		// an account created through the form gets - and this wrote a fixed eight
		// instead. That is the same hours today, and not the same account: the list
		// showed a figure where every other row showed "default", the owner's form
		// held a value they had never chosen, and a change to the default would pass
		// it by. The ceiling was already left at zero here, so the same account had
		// one figure pinned and one following.
		_, err := s.users.Save(ctx, &model.User{
			Name:       name,
			Email:      email,
			RoleID:     role.ID,
			IsExternal: true,
			ExternalID: directoryUser.ID,
		})
		if err != nil {
			return err
		}

		// After the write, not before it. Recording an account as created and
		// then failing to create it is the report saying something that did not
		// happen - and this report is what a caller logs and what the screen shows.
		report.Created = append(report.Created, email)

		s.count(ctx, MetricDirectoryAccounts, "action", "created")
	}

	return nil
}

// deleteRatio is the administered safety limit, or the configured one.
func (s *LDAPSyncService) deleteRatio(ctx context.Context) float64 {
	if s.limits == nil {
		return s.maxDeleteRatio
	}

	return s.limits.Limits(ctx).LDAPSyncMaxDeleteRatio
}

// WithLimits attaches the administered limits.
func (s *LDAPSyncService) WithLimits(limits *LimitsProvider) *LDAPSyncService {
	s.limits = limits

	return s
}

// countTimesheets is how many entries a departure would destroy.
//
// Through CountByFilter, which exists for this and says so: "Reading every row to
// count them is the cost this exists to avoid." It read them, and took len() of
// the slice.
//
// The preview is what makes that worth avoiding rather than the run. It is the
// screen somebody opens to decide, it happens before anything is deleted, and it
// pays this once per candidate - so an installation where a few long-serving
// people have left reads years of entries into memory to print a handful of
// numbers, on a machine that may be a Raspberry Pi.
func (s *LDAPSyncService) countTimesheets(ctx context.Context, userID uint) (int, error) {
	total, err := s.timesheets.CountByFilter(ctx, repository.TimesheetFilter{UserID: userID})
	if err != nil {
		return 0, err
	}

	return int(total), nil
}
