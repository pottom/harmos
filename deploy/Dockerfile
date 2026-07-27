# syntax=docker/dockerfile:1

# --- build stage ---------------------------------------------------------------
# Cross-compile on the build host's native arch (fast), targeting the requested
# platform. harmos's linux path is pure Go (CGO off), so it links statically —
# no libc in the final image.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG GIT_COMMIT=unknown
ARG BUILD_DATE=unknown

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath \
      -ldflags="-s -w \
        -X github.com/pottom/harmos/internal/version.Version=${VERSION} \
        -X github.com/pottom/harmos/internal/version.GitCommit=${GIT_COMMIT} \
        -X github.com/pottom/harmos/internal/version.BuildDate=${BUILD_DATE}" \
      -o /out/harmos ./cmd/harmos

# --- final stage ---------------------------------------------------------------
# distroless static: CA certificates for the Pleasant HTTPS sync, a nonroot user,
# and no shell or package manager — a minimal surface for a read-only password
# client. Run the TUI with `docker run -it`, or a CLI verb: `docker run … ls`.
FROM gcr.io/distroless/static:nonroot

LABEL org.opencontainers.image.source="https://github.com/pottom/harmos" \
      org.opencontainers.image.description="Read-only terminal password client for Pleasant Password Server and local .kdbx files" \
      org.opencontainers.image.licenses="MIT"

COPY --from=builder /out/harmos /usr/local/bin/harmos

# Mount config and cache here (owned by the nonroot user); sync needs the cache
# path writable.
ENV XDG_CONFIG_HOME=/home/nonroot/.config \
    XDG_DATA_HOME=/home/nonroot/.local/share

USER nonroot
ENTRYPOINT ["harmos"]
