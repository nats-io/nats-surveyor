FROM golang:1.18-alpine3.16 AS build
COPY . /go/src/nats-surveyor
WORKDIR /go/src/nats-surveyor
ENV GO111MODULE=on
RUN go build

FROM alpine:latest as osdeps
RUN apk add --no-cache ca-certificates

FROM alpine:3.16
COPY --from=build /go/src/nats-surveyor/nats-surveyor /nats-surveyor
COPY --from=osdeps /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

USER root
WORKDIR /root

EXPOSE 7777
ENTRYPOINT ["/nats-surveyor"]
CMD ["--help"]
