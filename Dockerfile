FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/schedulebot ./cmd/schedulebot

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -u 10001 bot \
    && mkdir -p /app/data \
    && chown -R bot:bot /app
USER bot
WORKDIR /app
COPY --from=build /out/schedulebot /usr/local/bin/schedulebot
COPY templates ./templates
ENV DATA_FILE=/app/data/events.json AUTH_FILE=/app/data/auth.json TIMEZONE=Europe/Moscow TEMPLATE_DIR=/app/templates
VOLUME ["/app/data"]
ENTRYPOINT ["schedulebot"]
