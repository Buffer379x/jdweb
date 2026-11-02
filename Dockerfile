# syntax=docker/dockerfile:1
# JDWeb: a web interface for JDownloader (unofficial, not affiliated with AppWork).
# One static Go binary with the built web app inside, in a distroless image.
# Base images are pinned to an exact version and digest; bump them on purpose.

FROM --platform=$BUILDPLATFORM node:24.22.1-alpine3.24@sha256:06d7adf3e0ef61a13f1d28682726366f97a15aa1888c934d3ecec86c33918b53 AS frontend
# Same pnpm as "packageManager" in web/package.json.
RUN npm install --global pnpm@10.34.9
WORKDIR /src/web
COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

FROM --platform=$BUILDPLATFORM golang:1.27.3-alpine3.24@sha256:d200d2dd76b03a99b52651fd1325aff7dcd35422c35c01bac56571b0507f3deb AS backend
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=0.1.0
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /src/web/dist ./web/dist
# The build fails when VERSION (build argument) and the VERSION file disagree.
RUN test "$VERSION" = "$(cat VERSION)" \
 && CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/jdweb ./cmd/jdweb \
 && mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
ARG VERSION=0.1.0
ARG SOURCE=https://github.com/Buffer379x/jdweb
LABEL org.opencontainers.image.title="JDWeb" \
      org.opencontainers.image.description="Unofficial web interface for JDownloader. Not affiliated with AppWork." \
      org.opencontainers.image.source=$SOURCE \
      org.opencontainers.image.version=$VERSION \
      org.opencontainers.image.licenses="MIT"
COPY --from=backend /out/jdweb /jdweb
# /data belongs to 99:100 (nobody:users on Unraid) already, so that a named
# volume is writable without a chown. A bind mount that Docker created as root
# is handled by starting as root (--user 0:0): JDWeb gives it to PUID:PGID and
# drops root before it opens the database.
COPY --from=backend --chown=99:100 /out/data /data
ENV JDWEB_DATA_DIR=/data \
    JDWEB_LISTEN=:7468 \
    GOMEMLIMIT=128MiB \
    SQLITE_TMPDIR=/data
USER 99:100
VOLUME ["/data"]
EXPOSE 7468
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 CMD ["/jdweb", "healthcheck"]
ENTRYPOINT ["/jdweb"]
CMD ["serve"]
