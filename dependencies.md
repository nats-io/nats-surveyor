# External Dependencies

This file lists the dependencies used in this repository.

We restrict it to the dependencies pulled into the actual binary.

Update instructions

```sh
go install github.com/mitchellh/golicense@latest

go build
export GITHUB_TOKEN='...'
golicense nats-surveyor
golicense -plain nats-surveyor | sort > licenses

go list -f '{{with .Module}}{{if .Version}}{{.Path}}{{"\t"}}{{.Version}}{{end}}{{end}}' all | sort -u > versions

wc -l versions licenses

paste versions licenses | awk '{if (index($1,$3) == 1) {print "| " $3 " | " $2 " | " $4 " |"} else {print "MISMATCH"}}'

rm versions licenses
```

## Dependency Table

| Dependency | Version | License |
|-|-|-|
| github.com/beorn7/perks | v1.0.1 | MIT |
| github.com/cespare/xxhash | v2.1.2 | MIT |
| github.com/dustin/go-humanize | v1.0.0 | MIT |
| github.com/golang/protobuf | v1.5.2 | BSD |
| github.com/klauspost/compress | v1.14.2 | MIT |
| github.com/matttproud/golang_protobuf_extensions | v1.0.1 | Apache |
| github.com/minio/highwayhash | v1.0.2 | Apache |
| github.com/nats-io/jsm.go | v0.0.27 | Apache |
| github.com/nats-io/jwt | v2.2.1-0.20220113022732-58e87895b296 | Apache |
| github.com/nats-io/nats.go | v1.13.1-0.20220121202836-972a071d373d | Apache |
| github.com/nats-io/nats-server | v2.7.1 | Apache |
| github.com/nats-io/nkeys | v0.3.0 | Apache |
| github.com/nats-io/nuid | v1.0.1 | Apache |
| github.com/prometheus/client_golang | v1.12.0 | Apache |
| github.com/prometheus/client_model | v0.2.0 | Apache |
| github.com/prometheus/common | v0.32.1 | Apache |
| github.com/prometheus/procfs | v0.7.3 | Apache |
| golang.org/x/crypto | v0.0.0-20220126234351-aa10faf2a1f8 | BSD |
| golang.org/x/sys | v0.0.0-20220114195835-da31bd327af9 | BSD |
| golang.org/x/time | v0.0.0-20211116232009-f0f3c7e86c11 | BSD |
| google.golang.org/protobuf | v1.27.1 | BSD |