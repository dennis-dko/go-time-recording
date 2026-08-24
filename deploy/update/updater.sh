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
    # The image the running container was made from, before anything changes it.
    # Kept so the one it replaces can be removed by id afterwards - `image prune`
    # would take every dangling image on the host, including ones belonging to
    # somebody else's deployment.
    old=$(docker compose images --quiet "$SERVICE" 2>/dev/null | head -n 1 || true)

    log "pulling"

    if ! pulled=$(docker compose pull "$SERVICE" 2>&1); then
        log "pull failed: $pulled"
        finish "failed: $(echo "$pulled" | tail -n 3)"

        return
    fi

    new=$(docker compose images --quiet "$SERVICE" 2>/dev/null | head -n 1 || true)

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
