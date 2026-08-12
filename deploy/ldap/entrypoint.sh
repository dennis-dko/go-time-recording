#!/bin/sh
# Builds the configuration from the environment, seeds the structure once, then
# runs slapd in the foreground.
#
# The seed is loaded offline with slapadd rather than over the network with
# ldapadd. That way the server never comes up half-populated: by the time
# anything can connect, either the structure is there or the container has
# already exited with the reason.
set -eu

CONF=/etc/ldap/slapd.conf
DATA=/var/lib/ldap
SEED=/seed

# Refused rather than defaulted. A directory that comes up on a password
# somebody did not choose is a directory that stays on it, and this one holds
# the credentials the whole installation signs in with.
: "${LDAP_ADMIN_PASSWORD:?set LDAP_ADMIN_PASSWORD - see deploy/.env.example}"
: "${LDAP_BIND_PASSWORD:?set LDAP_BIND_PASSWORD - see deploy/.env.example}"

SUFFIX="${LDAP_SUFFIX:-dc=example,dc=com}"
ORGANISATION="${LDAP_ORGANISATION:-Example Organisation}"
BINDDN_CN="${LDAP_BIND_CN:-service}"

# The first dc= component, which the suffix entry has to carry as its own dc
# attribute. "dc=example,dc=com" gives "example"; anything else is a suffix
# this file cannot build an entry for, so it says so rather than producing one
# slapadd will reject with a schema error nobody can read.
DC=$(printf '%s' "$SUFFIX" | sed -n 's/^dc=\([^,]*\).*/\1/p')
if [ -z "$DC" ]; then
    echo "LDAP_SUFFIX must begin with dc=, for example dc=example,dc=com" >&2
    echo "got: $SUFFIX" >&2
    exit 1
fi

mkdir -p /var/run/slapd
chown -R openldap:openldap /var/run/slapd "$DATA"

# Hashed rather than written as they arrive. Both files are readable inside the
# container for as long as it runs, and a plain password in one of them is a
# password anybody who can exec into it can read - including out of a copy of
# the image layer, if the config is ever baked in by mistake.
ROOTPW=$(slappasswd -s "$LDAP_ADMIN_PASSWORD")
BINDPW=$(slappasswd -s "$LDAP_BIND_PASSWORD")
ROOTDN="cn=admin,$SUFFIX"

# Exported so awk can read them out of ENVIRON below. Not passed as an
# assignment prefix on the awk call, which reads as though it were building
# them there and is not: the prefix is evaluated by this shell, so a value
# referring to another one in the same prefix would silently see the old value.
export SUFFIX ROOTDN ROOTPW DC ORGANISATION BINDDN_CN BINDPW

# awk with the values in the environment rather than sed with them in the
# expression. A password hash contains / and $ and every sed expression ever
# written for this eventually meets one; ENVIRON is never interpreted.
substitute() {
    template=$1
    output=$2

    awk '{
        gsub(/__ROOTPW__/, ENVIRON["ROOTPW"])
        gsub(/__BINDPW__/, ENVIRON["BINDPW"])
        gsub(/__ROOTDN__/, ENVIRON["ROOTDN"])
        gsub(/__SUFFIX__/, ENVIRON["SUFFIX"])
        gsub(/__ORGANISATION__/, ENVIRON["ORGANISATION"])
        gsub(/__BINDDN_CN__/, ENVIRON["BINDDN_CN"])
        gsub(/__DC__/, ENVIRON["DC"])
        print
    }' "$template" > "$output"
}

substitute "$SEED/slapd.conf.template" "$CONF"
chmod 600 "$CONF"
chown openldap:openldap "$CONF"

# data.mdb marks an already-seeded volume. Importing twice would fail on the
# first duplicate entry and take the container down on a plain restart - which
# is how a directory that worked for a month disappears on a host reboot.
if [ ! -f "$DATA/data.mdb" ]; then
    echo "seeding $SUFFIX"

    substitute "$SEED/structure.ldif.template" /tmp/structure.ldif

    # -q skips the index rebuild during the import; slapindex does it once at
    # the end, which is both faster and what makes the indexes usable at all.
    su openldap -s /bin/sh -c "slapadd -q -f $CONF -l /tmp/structure.ldif"

    rm -f /tmp/structure.ldif

    echo "seeded the suffix, ou=people, ou=services and cn=$BINDDN_CN"

    # Anything mounted at /seed/extra, in name order, after the structure above
    # so a file can put people into the ou=people it creates.
    #
    # Not scaffolding for the tests, though that is what uses it here: standing a
    # directory up from an export is how anybody would fill an empty one, and
    # doing it offline with slapadd rather than over the network with ldapadd is
    # what keeps the server from ever answering half-populated. Substituted like
    # the structure is, so a seed can be written against __SUFFIX__ rather than
    # against one installation's own.
    extra=0
    for file in "$SEED"/extra/*.ldif; do
        [ -e "$file" ] || break

        echo "seeding from $(basename "$file")"
        substitute "$file" /tmp/extra.ldif
        su openldap -s /bin/sh -c "slapadd -q -f $CONF -l /tmp/extra.ldif"
        rm -f /tmp/extra.ldif

        extra=$((extra + 1))
    done

    su openldap -s /bin/sh -c "slapindex -f $CONF"

    if [ "$extra" -eq 0 ]; then
        echo "there are no people in it yet - see deploy/compose.ldap.yaml"
    fi
else
    # Deliberately not re-seeded, and deliberately said out loud: a changed
    # LDAP_SUFFIX or LDAP_BIND_CN in .env does nothing to a directory that
    # already exists, and finding that out from a failing bind is worse than
    # reading it here.
    echo "directory already seeded, keeping what is on the volume"
    echo "LDAP_SUFFIX, LDAP_ORGANISATION and LDAP_BIND_CN are not re-applied"
fi

echo "starting slapd on ldap://0.0.0.0:389 with suffix $SUFFIX"

# exec so slapd becomes PID 1 and receives the stop signal directly; without it
# docker stop waits out the full timeout on every shutdown.
exec slapd -f "$CONF" -h "ldap://0.0.0.0:389/" -u openldap -g openldap \
    -d "${SLAPD_LOG_LEVEL:-0}"
