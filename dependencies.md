# External Dependencies

This file lists the dependencies used in this repository.

We restrict it to the dependencies pulled into the actual binary.

Update instructions

```sh
go install github.com/mitchellh/golicense@latest

go build
export GITHUB_TOKEN='...'
golicense -plain nats-surveyor | sort > licenses

go list -f '{{with .Module}}{{if .Version}}{{.Path}}{{"\t"}}{{.Version}}{{end}}{{end}}' all | sort -u > versions

wc -l versions licenses

paste versions licenses | awk '{if (index($1,$3) == 1) {print "| " $3 " | " $2 " | " $4 " |"} else {print "MISMATCH"}}'

rm versions licenses
```

## Dependency Table

| Dependency                                                       | License      |
|------------------------------------------------------------------|--------------|
| github.com/beorn7/perks/quantile                                 | MIT          |
| github.com/cespare/xxhash/v2                                     | MIT          |
| github.com/dustin/go-humanize                                    | MIT          |
| github.com/fsnotify/fsnotify                                     | BSD-3-Clause |
| github.com/golang/protobuf                                       | BSD-3-Clause |
| github.com/hashicorp/hcl                                         | MPL-2.0      |
| github.com/klauspost/compress/s2                                 | BSD-3-Clause |
| github.com/magiconair/properties                                 | BSD-2-Clause |
| github.com/matttproud/golang_protobuf_extensions/pbutil          | Apache-2.0   |
| github.com/minio/highwayhash                                     | Apache-2.0   |
| github.com/mitchellh/mapstructure                                | MIT          |
| github.com/nats-io/jsm.go                                        | Apache-2.0   |
| github.com/nats-io/jwt/v2                                        | Apache-2.0   |
| github.com/nats-io/nats-server/v2                                | Apache-2.0   |
| github.com/nats-io/nats-surveyor                                 | Apache-2.0   |
| github.com/nats-io/nats.go                                       | Apache-2.0   |
| github.com/nats-io/nkeys                                         | Apache-2.0   |
| github.com/nats-io/nuid                                          | Apache-2.0   |
| github.com/pelletier/go-toml/v2                                  | MIT          |
| github.com/prometheus/client_golang/prometheus                   | Apache-2.0   |
| github.com/prometheus/client_model/go                            | Apache-2.0   |
| github.com/prometheus/common                                     | Apache-2.0   |
| github.com/prometheus/common/internal/bitbucket.org/ww/goautoneg | BSD-3-Clause |
| github.com/prometheus/procfs                                     | Apache-2.0   |
| github.com/spf13/afero                                           | Apache-2.0   |
| github.com/spf13/cast                                            | MIT          |
| github.com/spf13/cobra                                           | Apache-2.0   |
| github.com/spf13/jwalterweatherman                               | MIT          |
| github.com/spf13/pflag                                           | BSD-3-Clause |
| github.com/spf13/viper                                           | MIT          |
| github.com/subosito/gotenv                                       | MIT          |
| golang.org/x/crypto                                              | BSD-3-Clause |
| golang.org/x/sys                                                 | BSD-3-Clause |
| golang.org/x/text                                                | BSD-3-Clause |
| golang.org/x/time/rate                                           | BSD-3-Clause |
| google.golang.org/protobuf                                       | BSD-3-Clause |
| gopkg.in/ini.v1                                                  | Apache-2.0   |
| gopkg.in/yaml.v3                                                 | MIT          |
