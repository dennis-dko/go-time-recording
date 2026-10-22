package model

import "time"

// AdminEvent is one change somebody made to how the installation is run: an
// account, a role, a setting, a restart.
//
// Who and whom are ids and nothing else. A name is read when the entry is shown,
// so a renamed account appears under its new name and an erased one under none -
// the entry outlives the account, as a record of administration has to, without
// keeping the person it named. Ids are never handed out twice, so an erased one
// names nobody.
type AdminEvent struct {
	ID uint
	At time.Time

	// ActorID is the account that made the change.
	ActorID uint

	// Action names the change, as the administration log's table of routes calls
	// it: "user.created", "settings.branding".
	Action string

	// TargetID is the account or role the change was made to, zero for a change to
	// the installation as a whole.
	TargetID uint

	// Detail is the one value of the change worth keeping beside it - the role an
	// account was given, whether maintenance went on or off - and never a name of
	// a person.
	Detail string
}
