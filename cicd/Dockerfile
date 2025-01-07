#syntax=docker/dockerfile-upstream:1.6
FROM goreleaser as build

ARG GO_APP

ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT

RUN mkdir -p /go/bin /go/src

WORKDIR /go/src

RUN <<EOT
  set -e 
  GORELEASER_DIST_DIR="/go/src/dist"

  if [ "${TARGETARCH}" = "arm" ]; then VARIANT="$(echo "${TARGETVARIANT}" | sed 's/^v//')"; fi
  BIN_PATH=$(jq -r ".[] |select(.type   == \"Binary\" and \
                                .goos   == \"${TARGETOS}\" and \
                                .goarch == \"${TARGETARCH}\" and \
                                (.goarm == \"${VARIANT}\" or .goarm == null) and \
                                .extra.ID == \"${GO_APP}\") | .path" < "${GORELEASER_DIST_DIR}/artifacts.json")
  cp ${BIN_PATH} /go/bin
EOT

FROM alpine:3.21.1

ARG GO_APP
ENV GO_APP ${GO_APP}

COPY --from=build --chmod=755 /go/bin/${GO_APP} /usr/local/bin/${GO_APP}

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

COPY --from=assets entrypoint.sh /entrypoint.sh

RUN ln -s /usr/local/bin/${GO_APP} /${GO_APP} && chmod +x /entrypoint.sh

ENTRYPOINT ["/entrypoint.sh"]
