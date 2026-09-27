// Package update tests the script that holds the Docker socket.
//
// Not with Docker. What can be proven without a daemon is the half that decides
// things: what the script does when a request appears, what it writes back,
// what it refuses, and - the one that matters most - whether a failure leaves
// the running installation alone. A stub on the PATH answers as the docker
// command would, and records what it was asked.
//
// The half that cannot be proven here is whether `docker compose up -d --no-deps`
// does what the comment says it does. That is Docker's behaviour rather than
// this script's, and the place it is proven is a deployment.
package update
