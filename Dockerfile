FROM golang:1.24 AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/cloudpulse ./cmd/cloudpulse

FROM alpine:3.22
WORKDIR /app
COPY --from=build /out/cloudpulse /app/cloudpulse
COPY migrations /app/migrations
EXPOSE 8080
ENTRYPOINT ["/app/cloudpulse"]
