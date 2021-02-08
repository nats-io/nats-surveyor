FROM golang:1.14-alpine3.13 as builder

WORKDIR $GOPATH/src/github.com/nats-io/

ENV GO111MODULE=on
RUN apk add -U --no-cache git binutils

RUN go get github.com/nats-io/nats-surveyor@v0.2.0
RUN strip /go/bin/*

# Final docker image building stage
FROM alpine:3.13

RUN apk add -U --no-cache ca-certificates figlet

COPY --from=builder /go/bin/nats-surveyor /nats-surveyor

USER root
WORKDIR /root

EXPOSE 7777
ENTRYPOINT ["/nats-surveyor"]
CMD ["--help"]
