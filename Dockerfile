FROM --platform=$BUILDPLATFORM golang:1.26.6-alpine3.24@sha256:3889b425f035be855a72fb4755265311293b6d414521f0a519d819df32222d83 AS build-stage
WORKDIR /build
ENV CGO_ENABLED=0
COPY go.mod go.sum ./
COPY ./internal/imports ./internal/imports
RUN go build ./internal/imports
COPY . .
RUN go test ./tests
RUN go version
ARG TARGETOS
ARG TARGETARCH
# -X injects the release version into the binary itself, so `rearm version`
# inside the container reports the real version. The CDN zip path achieves
# the same by sed-ing cmd/version.go in the publish workflow; the container
# build gets VERSION only as a build-arg, which ldflags turns into the same
# result without mutating sources.
ARG VERSION=not_versioned
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH go build \
    -ldflags "-X github.com/relizaio/rearm/cmd.Version=$VERSION" -o ./ ./...

# Skeleton for the scratch release image: directory tree, version file and
# the apprunner account, prepared here because scratch has no shell to run
# mkdir/adduser/echo in.
ARG CI_ENV=noci
ARG GIT_COMMIT=git_commit_undefined
ARG GIT_BRANCH=git_branch_undefined
RUN apk add --no-cache ca-certificates && \
    mkdir -p /skel/app/localdata /skel/indir /skel/outdir /skel/tmp && \
    echo "version=$VERSION" > /skel/app/version && \
    echo "commit=$GIT_COMMIT" >> /skel/app/version && \
    echo "branch=$GIT_BRANCH" >> /skel/app/version && \
    echo "apprunner:x:1000:1000:apprunner:/app:/sbin/nologin" > /skel-passwd && \
    echo "apprunner:x:1000:" > /skel-group && \
    chown -R 1000:1000 /skel

# The binary is fully static (CGO_ENABLED=0), so the release image carries no
# distro at all: FROM scratch removes every Alpine package finding (openssl,
# busybox) along with the shell and package manager an attacker could use.
# Everything a distro stage used to do at runtime (mkdir/adduser/echo) is
# prepared in the build stage and copied in; CA certificates come along so
# HTTPS against ReARM keeps working.
FROM scratch AS release-stage
ARG CI_ENV=noci
ARG GIT_COMMIT=git_commit_undefined
ARG GIT_BRANCH=git_branch_undefined
ARG VERSION=not_versioned
COPY --from=build-stage /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build-stage /skel-passwd /etc/passwd
COPY --from=build-stage /skel-group /etc/group
COPY --from=build-stage /skel/ /
COPY --from=build-stage --chown=1000:1000 /build/rearm /app/app
USER apprunner
ENV TMPDIR=/tmp HOME=/app

LABEL git_commit=$GIT_COMMIT
LABEL git_branch=$GIT_BRANCH
LABEL ci_environment=$CI_ENV
LABEL org.opencontainers.image.version=$VERSION
LABEL org.opencontainers.image.vendor="Reliza Incorporated"
LABEL org.opencontainers.image.title="ReARM CLI"
LABEL org.opencontainers.image.source="https://github.com/relizaio/rearm-cli"
LABEL org.opencontainers.image.license="MIT"
LABEL org.opencontainers.image.url="https://rearmhq.com"
LABEL org.opencontainers.image.base.name="registry.relizahub.com/library/rearm-cli"

ENTRYPOINT ["/app/app"]