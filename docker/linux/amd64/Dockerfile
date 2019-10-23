# Golang binary building stage
FROM golang:1.13

# download the source
WORKDIR /go/src/github.com/nats-io/nats-surveyor
RUN git clone --branch master https://github.com/nats-io/nats-surveyor.git .

# build
RUN CGO_ENABLED=0 GO111MODULE=on GOOS=linux GOARCH=amd64 go build -v -a -tags netgo -installsuffix netgo -ldflags "-s -w"

# Final docker image building stage
FROM scratch
COPY --from=0 /go/src/github.com/nats-io/nats-surveyor/nats-surveyor /nats-surveyor
EXPOSE 7777
ENTRYPOINT ["/nats-surveyor"]
CMD ["--help"]
