FROM golang:1.25-bookworm AS builder

# govips uses cgo bindings to libvips, so both cgo and the libvips headers
# must be available at build time (the previous CGO_ENABLED=0 + no-libvips
# setup here did not actually compile - see README "Docker" section).
ENV GO111MODULE=on \
    CGO_ENABLED=1

RUN apt-get update && apt-get install -y --no-install-recommends \
    libvips-dev pkg-config \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /build
COPY . .
RUN go mod tidy
RUN go build --ldflags "-s -w" -o main .

FROM debian:bookworm-slim

# Runtime needs libvips' shared libraries (not the -dev headers) since the
# binary above is dynamically linked against them via cgo.
RUN apt-get update && apt-get install -y --no-install-recommends \
    libvips42 ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /www

COPY --from=builder /build/main /www/
COPY --from=builder /build/.env /www/.env
COPY --from=builder /build/public/ /www/public/
COPY --from=builder /build/resources/ /www/resources/

ENTRYPOINT ["/www/main"]
