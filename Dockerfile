# Used by goreleaser, which copies the prebuilt static binary into the image.
FROM alpine:3.21

RUN apk add --no-cache ca-certificates

COPY downmonitor /usr/local/bin/downmonitor

ENTRYPOINT ["/usr/local/bin/downmonitor"]
