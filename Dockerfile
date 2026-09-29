# catalog — built for linux/amd64 and linux/arm64.
#
# Base images are pinned by manifest-list INDEX digest (bo-platform DECISIONS.md
# 2026-09-29): cgr.dev/chainguard/go is go1.27.1, cgr.dev/chainguard/static is
# the nonroot runtime; both carry amd64 and arm64. A per-arch digest would build
# on one side of the CI-amd64 / lab-arm64 boundary and fail on the other.
#
# No '# syntax=' line on purpose: it would pull a dockerfile frontend image by
# floating tag. BuildKit's built-in frontend handles everything used here.

# The build stage runs on the BUILD platform and cross-compiles, so neither
# target architecture needs QEMU.
FROM --platform=$BUILDPLATFORM cgr.dev/chainguard/go@sha256:437e77100bb4ed52e039d6430d4a97a7ec55404abbd7ec3ef968b9e499bbda49 AS build
ARG TARGETOS
ARG TARGETARCH
# GOTOOLCHAIN=local: the image defaults to local+auto, which would silently
# download a newer toolchain if go.mod ever asked for one. Fail instead.
ENV GOTOOLCHAIN=local CGO_ENABLED=0 GOMODCACHE=/go/pkg/mod GOCACHE=/go/cache
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/go/cache \
    GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags='-s -w' -o /out/catalog ./cmd/catalog

FROM cgr.dev/chainguard/static@sha256:41e17ed83c594a64a9396b6ab96dd26d5ddc290dacf4c177464712ff21ad534f
COPY --from=build /out/catalog /catalog
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/catalog"]
