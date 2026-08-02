# A small OpenLDAP server, seeded from 01-seed.ldif.
#
# Built here rather than pulled, on purpose. Bitnami withdrew their OpenLDAP
# images from Docker Hub in 2025 and the popular osixia image is archived
# upstream, so a test environment that depends on either is one deprecation
# away from not starting. Everything this needs is in Debian's own package
# repository, and the whole configuration is the two files next to this one.
#
# Deliberately insecure and deliberately disposable: one trivial password, no
# TLS, no persistence. It exists to exercise a bind and a search.
FROM debian:12-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends slapd ldap-utils \
    && rm -rf /var/lib/apt/lists/*

# Debian's package ships a configured instance from its postinst prompts. None
# of that is wanted: the configuration below is explicit and version-
# controlled, so the shipped database is removed rather than reconfigured.
RUN rm -rf /etc/ldap/slapd.d /var/lib/ldap/* \
    && mkdir -p /var/lib/ldap /seed

COPY slapd.conf /etc/ldap/slapd.conf
COPY 01-seed.ldif /seed/01-seed.ldif
COPY entrypoint.sh /usr/local/bin/entrypoint.sh

RUN chmod +x /usr/local/bin/entrypoint.sh

EXPOSE 389

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
