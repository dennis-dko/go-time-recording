# A small OpenLDAP server for an installation that has no directory of its own.
#
# Built here rather than pulled, for the same reason test/ldap does it: Bitnami
# withdrew their OpenLDAP images from Docker Hub in 2025 and the popular osixia
# image is archived upstream, so a deployment that depends on either is one
# deprecation away from not starting. Everything this needs is in Debian's own
# package repository, and the whole configuration is the three files beside
# this one.
#
# This is NOT the disposable directory under test/. That one has a password in
# a file, no persistence and five invented people in it. This one takes its
# password from the environment, hashes it, stores nothing but the structure,
# and keeps its data on a volume. Read deploy/compose.ldap.yaml first: most
# installations should not run this at all.
FROM debian:12-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends slapd ldap-utils \
    && rm -rf /var/lib/apt/lists/*

# Debian's package ships a configured instance assembled from its postinst
# prompts. None of that is wanted here: the configuration is explicit and
# version-controlled, so the shipped database is removed rather than
# reconfigured around.
RUN rm -rf /etc/ldap/slapd.d /var/lib/ldap/* \
    && mkdir -p /var/lib/ldap /seed

COPY slapd.conf.template /seed/slapd.conf.template
COPY structure.ldif.template /seed/structure.ldif.template
COPY entrypoint.sh /usr/local/bin/entrypoint.sh

RUN chmod +x /usr/local/bin/entrypoint.sh

EXPOSE 389

# A directory that has lost its database is a directory nobody can sign in
# against, so the data lives on a volume the compose file declares. Named here
# as well, so running this image by hand does not quietly write into the
# container's own filesystem.
VOLUME ["/var/lib/ldap"]

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
