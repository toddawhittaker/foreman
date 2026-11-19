# GO_IMAGE has no default: the Makefile passes the pinned image, so the Go
# version is named in one place and checked by `make verify`.
ARG GO_IMAGE
# The build runs on the builder's own platform and cross-compiles, so an arm64
# image is built without emulation.
FROM --platform=$BUILDPLATFORM ${GO_IMAGE} AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
# What About shows (internal/version). The Makefile passes them from git;
# without them the build reads Go's build info and the page says so. Only the
# characters of a version are kept, whatever the arguments hold, and the
# values are only ever expanded inside double quotes, never as commands.
ARG VERSION=
ARG COMMIT=
ARG TARGETOS
ARG TARGETARCH
RUN v="$(printf '%s' "${VERSION}" | tr -cd 'A-Za-z0-9._+-')" && c="$(printf '%s' "${COMMIT}" | tr -cd 'A-Za-z0-9-')" && \
    CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" GOTOOLCHAIN=local go build -trimpath \
    -ldflags="-s -w -X github.com/toddawhittaker/foreman/internal/version.Version=${v} -X github.com/toddawhittaker/foreman/internal/version.Commit=${c}" \
    -o /out/foreman ./cmd/foreman

# Both bases are pinned by their multi-arch index digest, so each platform
# resolves to the same pinned content on every machine.
FROM gcr.io/distroless/static-debian13@sha256:1a64f46364620465ef9e9a8f5413757dfdf22695e050bbfd7303eeaf17a59383
ARG VERSION=
ARG COMMIT=
ARG SOURCE=https://github.com/toddawhittaker/foreman
LABEL org.opencontainers.image.title="Foreman" \
      org.opencontainers.image.description="Runs Factorio servers in Docker and manages worlds: a map with its own mods, settings and game version." \
      org.opencontainers.image.source="${SOURCE}" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT}"
COPY --from=build /out/foreman /usr/local/bin/foreman
# The game's user and group, so shared files need no ownership changes (SPEC 3.6).
USER 845:845
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s CMD ["/usr/local/bin/foreman", "healthcheck"]
ENTRYPOINT ["/usr/local/bin/foreman"]
CMD ["serve"]
