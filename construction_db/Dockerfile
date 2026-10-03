# syntax=docker/dockerfile:1

# ---- Build stage ----
FROM golang:1.25.5-alpine AS build
WORKDIR /src

# Download dependencies first so this layer is cached between builds.
# go.sum* is optional: it won't exist until the first dependency is added.
COPY go.mod go.sum* ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/api ./construction_db/cmd/api

# ---- Runtime stage ----
# Distroless: no shell, no package manager, runs as a non-root user.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/api /api
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/api"]