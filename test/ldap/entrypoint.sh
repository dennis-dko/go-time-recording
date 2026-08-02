#!/bin/sh
# Seeds the directory once, then runs slapd in the foreground.
#
# The seed is loaded offline with slapadd rather than over the network with
# ldapadd. That way the server never comes up in a half-populated state: by the
# time anything can connect, either every entry is there or the container has
# already exited with the reason.
set -eu

SEED=/seed/01-seed.ldif
CONF=/etc/ldap/slapd.conf
DATA=/var/lib/ldap

mkdir -p /var/run/slapd
chown -R openldap:openldap /var/run/slapd "$DATA"

# data.mdb marks an already-seeded volume. Importing twice would fail on the
# first duplicate entry and take the container down on a plain restart.
if [ ! -f "$DATA/data.mdb" ]; then
    echo "seeding the directory from $SEED"

    # -q skips the index rebuild during import; slapindex below does it once at
    # the end, which is both faster and what makes the indexes actually usable.
    su openldap -s /bin/sh -c "slapadd -q -f $CONF -l $SEED"
    su openldap -s /bin/sh -c "slapindex -f $CONF"

    echo "seeded $(grep -c '^dn:' "$SEED") entries"
else
    echo "directory already seeded, skipping the import"
fi

echo "starting slapd on ldap://0.0.0.0:389"

# exec so slapd becomes PID 1 and receives the stop signal directly; without it
# docker stop would wait out the full timeout on every shutdown.
exec slapd -f "$CONF" -h "ldap://0.0.0.0:389/" -u openldap -g openldap -d "${SLAPD_LOG_LEVEL:-0}"
