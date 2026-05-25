FROM --platform=$BUILDPLATFORM golang:1.24 AS build
ARG TARGETOS=linux
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=${TARGETARCH:-$(go env GOARCH)} go build -o /out/cloudpulse ./cmd/cloudpulse

FROM alpine:3.22
WORKDIR /app
COPY --from=build /out/cloudpulse /app/cloudpulse
COPY migrations /app/migrations
EXPOSE 8080
ENTRYPOINT ["/app/cloudpulse"]
