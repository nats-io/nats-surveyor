FROM golang:1.12-alpine3.10 as builder

WORKDIR $GOPATH/src/github.com/nats-io/

ENV GO111MODULE=on
RUN apk add -U --no-cache git binutils

RUN go get github.com/nats-io/nats-surveyor
RUN strip /go/bin/*

# Final docker image building stage
FROM alpine:3.10

RUN apk add -U --no-cache ca-certificates figlet

COPY --from=builder /go/bin/nats-surveyor /nats-surveyor

USER root
WORKDIR /root

EXPOSE 7777
ENTRYPOINT ["/nats-surveyor"]
CMD ["--help"]
