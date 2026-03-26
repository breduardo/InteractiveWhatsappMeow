# syntax=docker/dockerfile:1.7

FROM golang:1.25-alpine AS builder

WORKDIR /src

RUN apk add --no-cache ca-certificates git

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal
COPY migrations ./migrations
COPY public ./public

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/interactivewhatsmeow ./cmd/api

FROM alpine:3.22

WORKDIR /app

RUN addgroup -S app && adduser -S -G app app \
	&& apk add --no-cache ca-certificates tzdata

COPY --from=builder /out/interactivewhatsmeow /app/interactivewhatsmeow
COPY --from=builder /src/public /app/public

USER app

EXPOSE 3000

CMD ["/app/interactivewhatsmeow"]

