#!/bin/sh
#
# Recreates the application's container from a freshly pulled image, when asked.
#
# Asked by a file appearing in a shared volume, and by nothing else. There is no
# listener here, no argument to pass and no name to choose: the request is empty
# and the answer to it is always the same sentence - pull the image the compose
# file names, recreate the service named in GTR_UPDATE_SERVICE, remove the image
# that was replaced.
#
# That narrowness is the whole design. This container holds the Docker socket,
# which is root on the host; see compose.update.yaml for why it is here and not
# in the application.
#
# The protocol, in one directory:
#
#   request   written by the application, removed by this. Empty.
#   running   written by this while it works, so a second press does nothing.
#   result    written by this when it is over: "ok", "none" or "failed: ...".
#
# The application reads `result` and shows it. It usually does not get to read a
# successful one, because a successful update replaces the container that would
# have read it - the browser finds out by watching the version come back
# different, which is what it does after any restart. A failure leaves the old
# container running, and that one can read.

set -eu

REQUESTS="${GTR_UPDATE_REQUESTS:-/var/lib/gtr/update}"
SERVICE="${GTR_UPDATE_SERVICE:-app}"
POLL="${GTR_UPDATE_POLL:-3}"

log() { echo "[updater] $*"; }

# finish clears the working marker and then publishes the outcome.
#
# In that order, and the order is the whole of it: the outcome is what the other
# side waits for, so it has to be the last thing that becomes true. Written the
# other way round there is a moment where the result is readable and the marker
# still says an update is running - and a reader that believes both refuses the
# next request forever.
#
# The result itself is moved into place rather than written there, so a reader
# polling for it cannot find it half-written.
finish() {
    rm -f "$REQUESTS/running"

    printf '%s' "$1" > "$REQUESTS/result.tmp"
    mv "$REQUESTS/result.tmp" "$REQUESTS/result"
}

update() {
    # What is running, and what the tag it came from points at.
    #
    # Two different questions, and the first version of this asked the first one
    # twice. `docker compose images` reports the image of the *container* - which
    # a pull does not change, because the container goes on running what it
    # started with. So "did the pull find anything" was answered by comparing the
    # running image against itself, which is always "no", and this would have
    # updated nothing, ever. It reported "already on the newest image" on an
    # installation a day behind, which is the most convincing way to be wrong.
    #
    # The container's own record has both: .Image is what it runs, .Config.Image
    # is the reference it was created from.
    container=$(docker compose ps -q "$SERVICE" 2>/dev/null | head -n 1 || true)

    old=""
    reference=""

    if [ -n "$container" ]; then
        old=$(docker inspect -f '{{.Image}}' "$container" 2>/dev/null || true)
        reference=$(docker inspect -f '{{.Config.Image}}' "$container" 2>/dev/null || true)
    fi

    log "pulling"

    if ! pulled=$(docker compose pull "$SERVICE" 2>&1); then
        log "pull failed: $pulled"
        finish "failed: $(echo "$pulled" | tail -n 3)"

        return
    fi

    # What the tag points at now. Where there is no container to ask - the
    # application is stopped - there is nothing to compare and `up` is the right
    # answer anyway, so this stays empty and the recreate goes ahead.
    new=""

    if [ -n "$reference" ]; then
        new=$(docker image inspect -f '{{.Id}}' "$reference" 2>/dev/null || true)
    fi

    # Nothing was published since the last time. Said rather than done: a
    # recreate that changes nothing still interrupts everybody using it.
    if [ -n "$old" ] && [ "$old" = "$new" ]; then
        log "already on the newest image"
        finish "none"

        return
    fi

    log "recreating $SERVICE"

    # --no-deps, because this is not a deployment. The database beside the
    # application is not being updated and must not be restarted underneath it;
    # and this container is part of the same project, so an unscoped `up` would
    # recreate the updater in the middle of the update it is performing.
    if ! recreated=$(docker compose up -d --no-deps "$SERVICE" 2>&1); then
        log "recreate failed: $recreated"
        finish "failed: $(echo "$recreated" | tail -n 3)"

        return
    fi

    # The image that was replaced. Failure here is not a failure of the update:
    # something else on this host may be using it, and an update that worked
    # must not be reported as broken because a cleanup could not run.
    if [ -n "$old" ] && [ "$old" != "$new" ]; then
        if docker image rm "$old" >/dev/null 2>&1; then
            log "removed the image that was replaced"
        else
            log "the replaced image is still in use by something and was kept"
        fi
    fi

    log "done"
    finish "ok"
}

# The project has to be at the same path here as it is on the host.
#
# Everything this container does with compose ends at the daemon, and the daemon
# is outside. A compose file's bind mounts are written relative to the project
# directory and turned into absolute paths before they are sent - so if the
# project is at /project in here and at /home/you/app out there, every bind the
# application has is computed as a path the host does not have. Docker's answer
# to a bind source that does not exist is to create an empty directory and mount
# it, which means the application comes up with empty mounts and says nothing.
#
# That is not a theory. The first deployment of this recreated an application
# whose certificates are a bind mount, and it came back serving plain HTTP,
# reporting itself healthy, with an empty /certs.
#
# So it is checked, from the one place that knows both answers: this container's
# own record at the daemon, which lists what each mount is bound from. If the
# source and the destination of the project mount are not the same path, nothing
# here is safe to do.
verify_paths() {
    # Docker sets HOSTNAME to the container's id, which is what the daemon
    # answers to. A deployment that sets a hostname of its own breaks that, and
    # the answer to an unidentifiable container is to say so and carry on rather
    # than to refuse: this guards against one specific mistake, and a guard that
    # stops working installations is worse than the mistake it prevents.
    own="${HOSTNAME:-$(cat /etc/hostname 2>/dev/null || true)}"
    here=$(pwd)

    if [ -z "$own" ]; then
        log "cannot identify this container; the project path is unchecked"
        return 0
    fi

    template="{{range .Mounts}}{{if eq .Destination \"$here\"}}{{.Source}}{{end}}{{end}}"
    there=$(docker inspect -f "$template" "$own" 2>/dev/null || true)

    if [ -z "$there" ]; then
        log "cannot read this container's mounts; the project path is unchecked"
        return 0
    fi

    if [ "$there" != "$here" ]; then
        log "the project is $there on the host and $here in here."
        log "every bind mount the application has would be computed against a path"
        log "the host does not have, and Docker would create it empty - an"
        log "application recreated that way comes up with nothing mounted."
        log "set GTR_PROJECT_DIR to $there and start this again."
        return 1
    fi

    log "the project is at $here on both sides"
    return 0
}

if ! verify_paths; then
    log "refusing to run"

    # Slowly, rather than in a restart loop that fills the log faster than
    # anybody can read the sentence explaining what to fix.
    while true; do sleep 3600; done
fi

# The shared directory has to be writable by the application, which is not root.
#
# A named volume is created owned by root, and the application runs as an
# unprivileged user - so without this it can see the directory, fail to write
# into it, and correctly report that no updater is available. The button would
# simply never appear, which is a hard thing to work out from the outside.
#
# Done here rather than in the application's image: this is the updater granting
# the application the ability to ask it for something, so it belongs on this
# side of the arrangement. It also fixes a volume that already exists, which a
# line in a Dockerfile could not.
#
# The application checks whether it can write on every look rather than once at
# start-up, so a container that came up before this ran finds the button a
# moment later without being restarted.
mkdir -p "$REQUESTS"
chown "${GTR_UPDATE_OWNER:-10001}" "$REQUESTS" 2>/dev/null ||
    log "could not give $REQUESTS to uid ${GTR_UPDATE_OWNER:-10001}; the application may not be able to ask"

log "watching $REQUESTS for $SERVICE, every ${POLL}s"

# Anything left behind by a previous life of this container. A `running` marker
# that outlived the process that wrote it would refuse every future request.
rm -f "$REQUESTS/running"

while true; do
    if [ -f "$REQUESTS/request" ]; then
        # Taken before the work starts, so a second press while this runs finds
        # no request to make and the marker saying why.
        rm -f "$REQUESTS/request" "$REQUESTS/result"
        : > "$REQUESTS/running"

        update
    fi

    sleep "$POLL"
done
