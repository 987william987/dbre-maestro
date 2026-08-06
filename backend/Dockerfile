FROM golang:1.25-alpine AS builder
WORKDIR /app
RUN apk --no-cache add build-base
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-s -w" -o maestro ./cmd/server

FROM golang:1.25-alpine AS my2sql-builder
ARG MY2SQL_REF=master
RUN apk --no-cache add git
WORKDIR /src/my2sql
RUN git clone https://github.com/liuhr/my2sql.git . \
    && git checkout "$MY2SQL_REF" \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/my2sql .

FROM alpine:3.20
RUN apk --no-cache add ca-certificates wget
WORKDIR /app
COPY --from=builder /app/maestro .
COPY --from=my2sql-builder /out/my2sql /usr/local/bin/my2sql
COPY migrations/ ./migrations/
EXPOSE 8080
ENTRYPOINT ["./maestro"]
