FROM golang:1.24-alpine3.21 AS build
COPY . /go/src/nats-surveyor
WORKDIR /go/src/nats-surveyor
ENV GO111MODULE=on
ENV CGO_ENABLED=0
RUN go build

FROM alpine:latest as osdeps
RUN apk add --no-cache ca-certificates

FROM alpine:3.21
COPY --from=build /go/src/nats-surveyor/nats-surveyor /nats-surveyor
COPY --from=osdeps /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

USER root
WORKDIR /root

EXPOSE 7777
ENTRYPOINT ["/nats-surveyor"]
CMD ["--help"]
