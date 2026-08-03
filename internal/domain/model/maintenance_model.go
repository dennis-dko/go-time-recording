package model

import "strings"

// Maintenance takes the installation out of service without stopping it.
//
// The point is to keep people from recording time against a database that is
// about to be restored, migrated or moved - work done during that window is
// silently lost when the snapshot is put back, and whoever did it has no way to
// know. Stopping the process would work too, but then nobody can see why it is
// down, and the administrator who has to turn it back on has nothing to turn on.
//
// So the application keeps serving and refuses the work instead.
type Maintenance struct {
	// Enabled turns it on.
	Enabled bool `json:"enabled"`

	// Message is shown to everyone who is turned away. Optional, and worth
	// filling in: "back at 14:00" stops the calls that "temporarily unavailable"
	// invites.
	Message string `json:"message"`
}

// MaintenanceMessageLimit bounds the message. Long enough for a sentence and a
// time, short enough that it cannot become a page nobody reads.
const MaintenanceMessageLimit = 300

// DefaultMaintenanceMessage is what is shown when no message was given.
const DefaultMaintenanceMessage = "This installation is temporarily unavailable for maintenance."

// Text is the message to show, falling back to the default.
func (m Maintenance) Text() string {
	if trimmed := strings.TrimSpace(m.Message); trimmed != "" {
		return trimmed
	}

	return DefaultMaintenanceMessage
}

// Normalise trims the message and cuts it to the limit.
//
// Cut rather than rejected: an over-long message is a mistake worth correcting
// quietly, and refusing the save would leave maintenance mode off while somebody
// edits their sentence - which is the opposite of what they were trying to do.
func (m Maintenance) Normalise() Maintenance {
	m.Message = strings.TrimSpace(m.Message)

	if len(m.Message) > MaintenanceMessageLimit {
		m.Message = strings.TrimSpace(m.Message[:MaintenanceMessageLimit])
	}

	return m
}
