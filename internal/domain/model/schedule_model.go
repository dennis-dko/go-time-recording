package model

import (
	"strconv"
	"strings"
)

// ScheduleFields is how many components a schedule has: minute, hour, day of
// month, month, day of week.
//
// The framework also accepts a six-part form with seconds. It is not offered
// here: a directory reconciliation that deletes accounts is not something to run
// by the second, and the narrower grammar is the one that can be explained on a
// screen.
const ScheduleFields = 5

// The inclusive bounds of each field, in order.
var scheduleBounds = [ScheduleFields][2]int{
	{0, 59}, // minute
	{0, 23}, // hour
	{1, 31}, // day of month
	{1, 12}, // month
	{0, 6},  // day of week
}

// ValidSchedule reports whether a cron expression is one the scheduler will
// actually accept.
//
// Checked before it is stored, because the framework's own answer to an
// expression it cannot read is a line in the log and no job - so an
// administrator who mistyped one would be told their nightly reconciliation was
// saved, and it would simply never run. The absence of a failure is
// indistinguishable from a directory that has not changed.
//
// An empty expression is valid and means "do not schedule it", which is the
// default and the safe one: a run deletes accounts the directory no longer
// holds, together with the hours recorded against them.
func ValidSchedule(expression string) bool {
	expression = strings.TrimSpace(expression)
	if expression == "" {
		return true
	}

	fields := strings.Fields(expression)
	if len(fields) != ScheduleFields {
		return false
	}

	for i, field := range fields {
		if !validScheduleField(field, scheduleBounds[i][0], scheduleBounds[i][1]) {
			return false
		}
	}

	return true
}

// validScheduleField checks one component: a list of items, each of which is a
// star, a step, a range or a number.
func validScheduleField(field string, minimum, maximum int) bool {
	if field == "" {
		return false
	}

	for _, item := range strings.Split(field, ",") {
		if !validScheduleItem(item, minimum, maximum) {
			return false
		}
	}

	return true
}

func validScheduleItem(item string, minimum, maximum int) bool {
	// A step: the part before the slash is a star or a range, the part after is
	// a positive number. "*/0" would divide the field into nothing.
	if base, step, found := strings.Cut(item, "/"); found {
		count, err := strconv.Atoi(step)
		if err != nil || count < 1 || count > maximum {
			return false
		}

		return base == "*" || validScheduleRange(base, minimum, maximum)
	}

	if item == "*" {
		return true
	}

	if strings.Contains(item, "-") {
		return validScheduleRange(item, minimum, maximum)
	}

	return validScheduleNumber(item, minimum, maximum)
}

// validScheduleRange checks "a-b", which has to run upwards: "5-1" describes no
// minutes at all rather than the eleven somebody probably meant.
func validScheduleRange(item string, minimum, maximum int) bool {
	from, to, found := strings.Cut(item, "-")
	if !found {
		return validScheduleNumber(item, minimum, maximum)
	}

	low, err := strconv.Atoi(from)
	if err != nil {
		return false
	}

	high, err := strconv.Atoi(to)
	if err != nil {
		return false
	}

	return low >= minimum && high <= maximum && low <= high
}

func validScheduleNumber(item string, minimum, maximum int) bool {
	value, err := strconv.Atoi(item)

	return err == nil && value >= minimum && value <= maximum
}
