package repository

import "context"

// ManagedAccountRepository keeps which accounts each manager has been assigned.
//
// Every method takes one id, on purpose. A question asked with a manager and an
// account side by side - "does this one look after that one" - compiles with the two
// swapped and answers the opposite question, which is the transposition CLAUDE.md
// warns about where it would be quiet; the lists are asked for and searched instead.
type ManagedAccountRepository interface {
	// ManagersOf returns the managers assigned to an account, in id order.
	ManagersOf(ctx context.Context, userID uint) ([]uint, error)

	// MembersOf returns the accounts assigned to a manager, in id order.
	MembersOf(ctx context.Context, managerID uint) ([]uint, error)

	// SetManagers replaces the managers assigned to an account.
	SetManagers(ctx context.Context, userID uint, managerIDs []uint) error
}
