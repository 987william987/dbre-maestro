FROM node:20-alpine AS frontend-builder

WORKDIR /src/frontend
COPY frontend/package*.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM golang:1.25-alpine AS backend-builder

WORKDIR /src/backend
RUN apk --no-cache add build-base
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-s -w" -o /out/maestro ./cmd/server

FROM golang:1.25-alpine AS my2sql-builder

ARG MY2SQL_REF=69b39554cb116d02fba389ff258ca9736dea7437
RUN apk --no-cache add git
WORKDIR /src/my2sql
RUN git clone https://github.com/liuhr/my2sql.git . \
    && git checkout "$MY2SQL_REF" \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/my2sql .

FROM alpine:3.20

RUN apk --no-cache add ca-certificates tzdata wget
WORKDIR /app

COPY --from=backend-builder /out/maestro /app/maestro
COPY --from=my2sql-builder /out/my2sql /usr/local/bin/my2sql
COPY --from=backend-builder /src/backend/migrations /app/migrations
COPY --from=frontend-builder /src/frontend/dist /app/public

ENV PORT=8080 \
    STATIC_DIR=/app/public

EXPOSE 8080
ENTRYPOINT ["/app/maestro"]
