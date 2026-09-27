// Package command holds the write requests the application layer accepts -
// what a caller is asking to create, change or delete, separated from however
// it arrived.
//
// One rule shapes most of these types, and it is the reason the package is
// worth a comment. A partial update built from pointers can say "leave this
// alone" and "set it to this", and nothing else. A field that has to be
// emptiable therefore needs its third state said out loud, as
// UpdateProjectCommand.ClearEndDate does. Never overload nil to mean "clear":
// every caller that stays silent about a field relies on nil meaning silence,
// the spreadsheet import among them.
//
// The ActorID carried alongside is who is asking, not who is being changed. The
// services use it to decide what may be seen and touched; zero means
// enforcement is switched off, which is the local trial case.
package command
