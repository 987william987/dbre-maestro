FROM golang:1.25-alpine AS builder
WORKDIR /app
RUN apk --no-cache add build-base
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-s -w" -o maestro ./cmd/server

FROM golang:1.25-alpine AS my2sql-builder
ARG MY2SQL_REF=69b39554cb116d02fba389ff258ca9736dea7437
RUN apk --no-cache add git
WORKDIR /src/my2sql
RUN git clone https://github.com/liuhr/my2sql.git . \
    && git checkout "$MY2SQL_REF" \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/my2sql .

FROM alpine:3.20 AS online-ddl-tools
ARG TARGETARCH
ARG GH_OST_VERSION=1.1.6
ARG GH_OST_RELEASE_DATE=20231207144046
ARG GH_OST_SHA256_AMD64=5d15547f207e72591fd3a55c9cbea275396880a65a290742287a1ac84d0f4977
ARG GH_OST_SHA256_ARM64=12f9d91a77774e85073fdea6bfb26f457424bf65b12043cb330e288231aa3465
ARG PERCONA_TOOLKIT_VERSION=3.7.1
ARG PERCONA_TOOLKIT_SHA256=6ba6b31ec437a6bce10df15a4e577f49f605ff8cb886557d0c7176ba7018b0cb
RUN apk --no-cache add ca-certificates curl tar \
    && mkdir -p /out \
    && case "$TARGETARCH" in amd64) gh_sha="$GH_OST_SHA256_AMD64" ;; arm64) gh_sha="$GH_OST_SHA256_ARM64" ;; *) exit 1 ;; esac \
    && curl -fsSL -o /tmp/gh-ost.tar.gz "https://github.com/github/gh-ost/releases/download/v${GH_OST_VERSION}/gh-ost-binary-linux-${TARGETARCH}-${GH_OST_RELEASE_DATE}.tar.gz" \
    && echo "${gh_sha}  /tmp/gh-ost.tar.gz" | sha256sum -c - \
    && tar -xzf /tmp/gh-ost.tar.gz -C /tmp \
    && install -m 0755 /tmp/gh-ost /out/gh-ost \
    && curl -fsSL -o /tmp/percona-toolkit.tar.gz "https://github.com/percona/percona-toolkit/archive/refs/tags/v${PERCONA_TOOLKIT_VERSION}.tar.gz" \
    && echo "${PERCONA_TOOLKIT_SHA256}  /tmp/percona-toolkit.tar.gz" | sha256sum -c - \
    && tar -xzf /tmp/percona-toolkit.tar.gz -C /tmp \
    && install -m 0755 "/tmp/percona-toolkit-${PERCONA_TOOLKIT_VERSION}/bin/pt-online-schema-change" /out/pt-online-schema-change

FROM alpine:3.20 AS runtime-base
RUN apk --no-cache add ca-certificates wget perl perl-dbi perl-dbd-mysql
WORKDIR /app
COPY --from=builder /app/maestro .
COPY --from=my2sql-builder /out/my2sql /usr/local/bin/my2sql
COPY --from=online-ddl-tools /out/gh-ost /usr/local/bin/gh-ost
COPY --from=online-ddl-tools /out/pt-online-schema-change /usr/local/bin/pt-online-schema-change
COPY migrations/ ./migrations/
EXPOSE 8080
ENTRYPOINT ["./maestro"]

FROM builder AS online-ddl-integration-builder
RUN go test -tags=integration -c -o /out/onlineddl-integration.test ./internal/onlineddl

FROM runtime-base AS online-ddl-integration
COPY --from=online-ddl-integration-builder /out/onlineddl-integration.test /app/onlineddl-integration.test
ENTRYPOINT ["/app/onlineddl-integration.test"]

FROM runtime-base AS runtime
