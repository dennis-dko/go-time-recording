# Jaeger, for exercising the trace exporter against something that actually
# stores spans.
#
# Unlike the directory next door, this wraps a maintained upstream image rather
# than replacing one that went away - so what it earns is narrower, and worth
# being honest about: the version, the receiver configuration and the health
# check live here, in one version-controlled place, instead of being spread
# across a compose file. The alternative, a bare `image:` line, would work too.
#
# Deliberately disposable: traces are kept in memory and are gone when the
# container stops. That is what makes it safe to throw away between runs, and
# useless for anything but testing.
FROM jaegertracing/all-in-one:1.62.0

# The OTLP receiver is what this exists for. GoFr's "otlp" and "jaeger"
# exporters are the same code path - both speak OTLP over gRPC on 4317 - so the
# native Jaeger protocol on 14250 is never used by this application.
ENV COLLECTOR_OTLP_ENABLED=true

# Bounded, so a test that exports in a loop cannot grow the container until the
# host notices. Ten thousand is far more than any test here produces.
ENV MEMORY_MAX_TRACES=10000

# The admin port answers with a readiness document rather than a bare 200, which
# is the difference between "the socket is open" and "the collector is ready to
# accept spans". Sent to 14269 rather than the query port because the query API
# answers before the receiver does, and a span exported into that gap is lost
# with no error anybody sees.
HEALTHCHECK --interval=3s --timeout=5s --retries=20 \
    CMD wget -q -O - http://127.0.0.1:14269/ | grep -q "Server available"

# 4317 OTLP/gRPC, 16686 the query API and the trace browser.
EXPOSE 4317 16686
