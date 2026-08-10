# syntax=docker/dockerfile:1

# ---------- Stage 1: build frontend ----------
FROM node:20-alpine AS frontend
WORKDIR /build
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

# ---------- Stage 2: build Go binary ----------
FROM golang:1.26-alpine AS build
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /build/dist ./frontend/dist
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
    -ldflags '-s -w -X main.VERSION=docker' \
    -o gohttpserver .

# ---------- Stage 3: runtime ----------
FROM alpine:3.21
RUN apk add --no-cache ca-certificates
WORKDIR /app
RUN mkdir -p /app/public
VOLUME /app/public
COPY --from=build /build/gohttpserver /usr/local/bin/gohttpserver
EXPOSE 8000
ENTRYPOINT ["/usr/local/bin/gohttpserver", "--root=/app/public"]
