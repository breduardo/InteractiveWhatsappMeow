# syntax=docker/dockerfile:1.7

FROM golang:1.25-bookworm AS builder

WORKDIR /src

COPY go.mod go.sum ./
COPY vendor ./vendor

COPY cmd ./cmd
COPY internal ./internal
COPY migrations ./migrations
COPY public ./public

RUN CGO_ENABLED=0 GOOS=linux go build -mod=vendor -trimpath -ldflags="-s -w" -o /out/interactivewhatsmeow ./cmd/api

FROM golang:1.25-bookworm

WORKDIR /app

COPY --from=builder --chown=65534:65534 /out/interactivewhatsmeow /app/interactivewhatsmeow
COPY --from=builder --chown=65534:65534 /src/public /app/public

USER 65534:65534

EXPOSE 3000

CMD ["/app/interactivewhatsmeow"]
