# A small OpenLDAP server, for an installation that has no directory of its own
# and for the tests.
#
# Built rather than pulled: Bitnami withdrew their OpenLDAP images from Docker
# Hub in 2025 and the popular osixia image is archived upstream, so anything
# depending on either is one deprecation away from not starting. Everything this
# needs is in Debian's own package repository, and the whole configuration is
# the three files beside this one.
#
# One image, used twice. There were two - this and a near-copy under test/ldap
# whose slapd.conf differed from this one's template by three substituted values
# and a maxsize. Nothing was gained by that and two things were lost: the same
# forty lines of directory configuration had to be corrected in two places, and
# the tests exercised a bind against an image no deployment ever runs. The test
# environment already treats PostgreSQL and MySQL this way, pulling the real
# image and handing it a throwaway password, and the directory was only ever
# special because there was no real image to pull.
#
# What differs between the two uses is inputs, not the image: test/ passes weak
# passwords and mounts a seed of five invented people into /seed/extra, and
# throws the whole thing away afterwards. Read deploy/compose.ldap.yaml before
# running this as a deployment - most installations should not.
FROM debian:12-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends slapd ldap-utils \
    && rm -rf /var/lib/apt/lists/*

# Debian's package ships a configured instance assembled from its postinst
# prompts. None of that is wanted here: the configuration is explicit and
# version-controlled, so the shipped database is removed rather than
# reconfigured around.
RUN rm -rf /etc/ldap/slapd.d /var/lib/ldap/* \
    && mkdir -p /var/lib/ldap /seed/extra

COPY slapd.conf.template /seed/slapd.conf.template
COPY structure.ldif.template /seed/structure.ldif.template
COPY entrypoint.sh /usr/local/bin/entrypoint.sh

RUN chmod +x /usr/local/bin/entrypoint.sh

EXPOSE 389

# No VOLUME for /var/lib/ldap, deliberately. A directory that has lost its
# database is one nobody can sign in against, so a deployment must keep it - and
# deploy/compose.ldap.yaml declares the volume that does. Declaring it here as
# well would force one on every other use of the image, including the tests,
# which want the data gone with the container and would instead accumulate an
# anonymous volume per run. Persistence is the deployment's decision, and the
# deployment is where it is written down.

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
