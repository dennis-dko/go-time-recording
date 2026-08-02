package service

import (
	"context"
	"fmt"
	"sort"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
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

	// Aborted explains why nothing was deleted, when a guard stopped the run.
	Aborted string

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
}

// UserPurger removes a user together with everything referencing them.
type UserPurger interface {
	PurgeUser(ctx context.Context, userID uint) error
}

// NewLDAPSyncService creates new instance.
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
		defaultRole = model.RoleEmployee
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
	return s.run(ctx, true)
}

// Sync reconciles the local accounts with the directory.
//
// Accounts the directory no longer holds are removed together with their time
// entries, private projects, tokens and sessions. Accounts the directory has
// and this installation does not are created.
func (s *LDAPSyncService) Sync(ctx context.Context) (*SyncReport, error) {
	return s.run(ctx, false)
}

// across helpers would scatter the reasons a destructive run is refused.
//
//nolint:cyclop // the guards are the point of this function; splitting them
func (s *LDAPSyncService) run(ctx context.Context, dryRun bool) (*SyncReport, error) {
	if !s.directory.Enabled() {
		return nil, apperror.Conflictf("no directory is configured")
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
		report.Aborted = "the directory returned no users at all; refusing to delete anyone"

		return report, nil
	}

	if reason := s.exceedsRatio(ctx, report); reason != "" {
		report.Aborted = reason

		return report, nil
	}

	if err := s.createMissing(ctx, directoryUsers, knownIDs, knownEmails, report, dryRun); err != nil {
		return nil, err
	}

	if dryRun {
		return report, nil
	}

	for _, candidate := range report.Candidates {
		if err := s.purger.PurgeUser(ctx, candidate.UserID); err != nil {
			return nil, err
		}

		report.Deleted = append(report.Deleted, candidate)
	}

	return report, nil
}

// stillInDirectory reports whether the directory still holds this account.
//
// The stable identifier decides whenever the account has one: a renamed
// mailbox then keeps matching, instead of looking like a departure that would
// take the person's recorded hours with it. Only an account with no identifier
// yet - one created before identifiers were recorded - falls back to the mail
// address.
func (s *LDAPSyncService) stillInDirectory(
	user *model.User,
	byID map[string]ExternalUser,
	byEmail map[string]ExternalUser,
) bool {
	if user.ExternalID != "" {
		_, found := byID[user.ExternalID]

		return found
	}

	_, found := byEmail[normalizeEmail(user.Email)]

	return found
}

// exceedsRatio reports why the run is refused when it would remove more of the
// directory-backed population than the configured share.
func (s *LDAPSyncService) exceedsRatio(ctx context.Context, report *SyncReport) string {
	ratioLimit := s.deleteRatio(ctx)
	if ratioLimit <= 0 || report.LocalExternal == 0 || len(report.Candidates) == 0 {
		return ""
	}

	ratio := float64(len(report.Candidates)) / float64(report.LocalExternal)
	if ratio <= ratioLimit {
		return ""
	}

	return fmt.Sprintf(
		"would remove %d of %d directory accounts (%.0f%%), above the %.0f%% safety limit; "+
			"check the directory filter and base DN, then raise LDAP_SYNC_MAX_DELETE_RATIO "+
			"if this really is intended",
		len(report.Candidates), report.LocalExternal, ratio*100, ratioLimit*100)
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
	role, err := s.roles.GetByName(ctx, s.defaultRole)
	if err != nil {
		return err
	}

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

		report.Created = append(report.Created, email)

		if dryRun {
			continue
		}

		name := directoryUser.Name
		if name == "" {
			name = email
		}

		_, err := s.users.Save(ctx, &model.User{
			Name:             name,
			Email:            email,
			RoleID:           role.ID,
			IsExternal:       true,
			ExternalID:       directoryUser.ID,
			DailyTargetHours: model.DefaultDailyTargetHours,
		})
		if err != nil {
			return err
		}
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

func (s *LDAPSyncService) countTimesheets(ctx context.Context, userID uint) (int, error) {
	entries, err := s.timesheets.GetByFilter(ctx, repository.TimesheetFilter{UserID: userID})
	if err != nil {
		return 0, err
	}

	return len(entries), nil
}
