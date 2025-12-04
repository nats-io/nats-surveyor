#syntax=docker/dockerfile:1.13
FROM goreleaser AS build

ARG GO_APP

ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT

RUN mkdir -p /go/bin /go/src

WORKDIR /go/src

RUN <<EOT
  set -e 
  GORELEASER_DIST_DIR="/go/src/dist"

  # If --single-target, artifacts.json only exists in os_arch target directory
  if [ ! -f ${GORELEASER_DIST_DIR}/artifacts.json ]; then
    GORELEASER_DIST_DIR="${GORELEASER_DIST_DIR}/${TARGETOS}_${TARGETARCH}"
  fi

  if [ "${TARGETARCH}" = "arm" ]; then VARIANT="$(echo "${TARGETVARIANT}" | sed 's/^v//')"; fi
  BIN_PATH=$(jq -r ".[] |select(.type   == \"Binary\" and \
                                .name   == \"${GO_APP}\" and \
                                .goos   == \"${TARGETOS}\" and \
                                .goarch == \"${TARGETARCH}\" and \
                                (.goarm == \"${VARIANT}\" or .goarm == null)) | .path" < "${GORELEASER_DIST_DIR}/artifacts.json")
  cp ${BIN_PATH} /go/bin
EOT

FROM alpine:3.23.0

ARG GO_APP
ENV GO_APP ${GO_APP}

COPY --from=build --chmod=755 /go/bin/${GO_APP} /usr/local/bin/${GO_APP}

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

COPY --from=assets entrypoint.sh /entrypoint.sh

RUN ln -s /usr/local/bin/${GO_APP} /${GO_APP} && chmod +x /entrypoint.sh

ENTRYPOINT ["/entrypoint.sh"]
