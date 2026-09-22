# Used by goreleaser, which provides the prebuilt static binary for each
# platform.
FROM alpine:3.21

ARG TARGETPLATFORM

RUN apk add --no-cache ca-certificates \
    && adduser -D -u 10001 downmonitor

COPY $TARGETPLATFORM/downmonitor /usr/local/bin/downmonitor

USER downmonitor

ENTRYPOINT ["/usr/local/bin/downmonitor"]
