# GO_IMAGE has no default: the Makefile passes the pinned image, so the Go
# version is named in one place and checked by `make verify`.
ARG GO_IMAGE
FROM ${GO_IMAGE} AS build
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
RUN v="$(printf '%s' "${VERSION}" | tr -cd 'A-Za-z0-9._+-')" && c="$(printf '%s' "${COMMIT}" | tr -cd 'A-Za-z0-9-')" && \
    CGO_ENABLED=0 GOTOOLCHAIN=local go build -trimpath \
    -ldflags="-s -w -X github.com/toddawhittaker/foreman/internal/version.Version=${v} -X github.com/toddawhittaker/foreman/internal/version.Commit=${c}" \
    -o /out/foreman ./cmd/foreman

FROM gcr.io/distroless/static-debian13@sha256:448810357cc8e67b7208988abdba0f5ec3d3aebee0a5b13eb624979792cffc0f
COPY --from=build /out/foreman /usr/local/bin/foreman
# The game's user and group, so shared files need no ownership changes (SPEC 3.6).
USER 845:845
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s CMD ["/usr/local/bin/foreman", "healthcheck"]
ENTRYPOINT ["/usr/local/bin/foreman"]
CMD ["serve"]
