FROM golang:latest AS builder

WORKDIR /build
COPY . .

RUN CGO_ENABLED=0 go build -o core-services cmd/main.go

FROM alpine:latest

LABEL org.opencontainers.image.source https://github.com/cirruslabs/core-services

COPY --from=builder /build/core-services /usr/local/bin/

ENTRYPOINT ["/usr/local/bin/core-services"]
