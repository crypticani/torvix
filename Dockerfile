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
COPY --from=build /out/torvix /app/torvix
COPY migrations /app/migrations
EXPOSE 8080
ENTRYPOINT ["/app/torvix"]
