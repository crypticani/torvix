FROM --platform=$BUILDPLATFORM golang:1.25 AS build
ARG TARGETOS=linux
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=${TARGETARCH:-$(go env GOARCH)} go build -o /out/torvix ./cmd/torvix

FROM alpine:3.22
LABEL org.opencontainers.image.title="Torvix" \
      org.opencontainers.image.description="Torvix is an open-source cloud cost intelligence and waste detection platform."
WORKDIR /app
RUN apk add --no-cache ca-certificates su-exec \
    && addgroup -S torvix \
    && adduser -S -D -H -G torvix torvix \
    && mkdir -p /app/logs \
    && chown -R torvix:torvix /app
COPY docker/entrypoint.sh /app/entrypoint.sh
RUN chmod 0755 /app/entrypoint.sh
COPY --from=build --chown=torvix:torvix /out/torvix /app/torvix
COPY --chown=torvix:torvix migrations /app/migrations
EXPOSE 8080
ENTRYPOINT ["/app/entrypoint.sh"]
