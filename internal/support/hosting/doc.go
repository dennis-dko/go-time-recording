// Package hosting answers questions about what this process is running inside.
//
// One question so far, and it moved here from the self-update package because a
// second caller appeared: whether restarting means replacing this process or
// letting something else start a new container. Neither of those is an update,
// and a restart primitive that imports the updater to find out where it is
// would be the wrong way round.
package hosting
