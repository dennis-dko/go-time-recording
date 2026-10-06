// Package hosting answers questions about what this process is running inside.
//
// The first moved here from the self-update package because a second caller
// appeared: whether restarting means replacing this process or letting something
// else start a new container. Neither of those is an update, and a restart
// primitive that imports the updater to find out where it is would be the wrong
// way round. The second is what to call the zone the process reads its clock in,
// which Go does not name.
package hosting
