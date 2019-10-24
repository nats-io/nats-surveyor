#!/bin/sh
#!/bin/sh

$(go env GOPATH)/bin/golangci-lint run \
          --no-config --exclude-use-default=false --max-same-issues=0 \
            --disable errcheck \
            --enable golint \
            --enable stylecheck \
            --enable interfacer \
            --enable unconvert \
            --enable dupl \
            --enable gocyclo \
            --enable gofmt \
            --enable goimports \
            --enable misspell \
            --enable unparam \
            --enable nakedret \
            --enable prealloc \
            --enable scopelint \
            --enable gocritic \
            --enable gochecknoinits \
            ./...
