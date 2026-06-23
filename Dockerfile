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

FROM alpine:3.20

RUN apk --no-cache add ca-certificates tzdata wget
WORKDIR /app

COPY --from=backend-builder /out/maestro /app/maestro
COPY --from=backend-builder /src/backend/migrations /app/migrations
COPY --from=frontend-builder /src/frontend/dist /app/public

ENV PORT=8080 \
    STATIC_DIR=/app/public

EXPOSE 8080
ENTRYPOINT ["/app/maestro"]
