// Package imageupdate asks something else to replace this container.
//
// A container deployment updates by pulling a new image and recreating the
// container from it. This process cannot do that: pulling and recreating need
// the Docker socket, and holding that socket is root on the host - not root in
// a container, root on the machine, because anything with it can start a
// container that mounts the host's filesystem. An application with a sign-in
// form is the last thing that should hold it.
//
// So the deployment can be given a second container that holds it instead,
// whose entire vocabulary is one sentence, and this is how that sentence is
// said: a file appears in a directory both can see. There is no argument to it.
// This side cannot name an image, cannot name a container and cannot pass a
// flag - it writes an empty file, and the updater does the one thing it does.
//
// See deploy/compose.update.yaml, which is where the privilege is granted and
// where the reasoning is written for the person granting it.
//
// Absent by default. Without the overlay there is no directory, this reports
// itself unavailable, and the version card offers what it offered before: the
// binary swapped inside the running container, which lasts until the container
// is recreated.
package imageupdate
