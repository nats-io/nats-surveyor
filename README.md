[![License][License-Image]][License-Url] [![Build][Build-Status-Image]][Build-Status-Url] [![Coverage][Coverage-Image]][Coverage-Url]

# NATS Surveyor

NATS Monitoring, Simplified.

NATS surveyor polls the NATS server for `Statz` messages to generate data for
Prometheus.  This allows a single exporter to connect to any NATS server and
get an entire picture of a NATS deployment without requiring extra monitoring
components or sidecars.  Surveyor has been used extensively by [Synadia](https://synadia.com).

[System accounts](https://docs.nats.io/nats-server/configuration/sys_accounts)
must be enabled to use surveyor.

## Usage

```text
  nats-surveyor [flags]

Flags:
  -s, --servers string                      NATS Cluster url(s) (default "nats://127.0.0.1:4222")
  -c, --count int                           Expected number of servers (-1 for undefined). (default 1)
      --timeout duration                    Polling timeout (default 3s)
      --server-discovery-timeout duration   Maximum wait time between responses from servers during server discovery.
                                            Use in conjunction with -count=-1. (default 500ms)
      --creds string                        Credentials File
      --nkey string                         Nkey Seed File
      --jwt string                          User JWT. Use in conjunction with --seed
      --seed string                         Private key (nkey seed). Use in conjunction with --jwt
      --user string                         NATS user name or token
      --password string                     NATS user password
      --tlscert string                      Client certificate file for NATS connections.
      --tlskey string                       Client private key for NATS connections.
      --tlscacert string                    Client certificate CA on NATS connections.
      --tlsfirst                            Whether to use TLS First connections.
  -p, --port int                            Port to listen on. (default 7777)
  -a, --addr string                         Network host to listen on. (default "0.0.0.0")
      --http-tlscert string                 Server certificate file (Enables HTTPS).
      --http-tlskey string                  Private key for server certificate (used with HTTPS).
      --http-tlscacert string               Client certificate CA for verification (used with HTTPS).
      --http-user string                    Enable basic auth and set user name for HTTP scrapes.
      --http-pass string                    Set the password for HTTP scrapes. NATS bcrypt supported.
      --prefix string                       Replace the default prefix for all the metrics.
      --observe string                      Listen for observation statistics based on config files in a directory.
      --jetstream string                    Listen for JetStream Advisories based on config files in a directory.
      --accounts                            Export per account metrics
      --gatewayz                            Export gateway metrics
      --sys-req-prefix string               Subject prefix for system requests ($SYS.REQ) (default "$SYS.REQ")
      --log-level string                    Log level, one of: trace|debug|info|warn|error|fatal|panic (default "info")
      --config string                       config file (default is ./nats-surveyor.yaml)
  -h, --help                                help for nats-surveyor
  -v, --version                             version for nats-surveyor
```

At this time, NATS 2.0 System credentials are required for meaningful usage. Those can be provided in 2 ways:

- using `--creds` option to supply chained credentials file (containing JWT and NKey seed):

```sh
./nats-surveyor --creds ./test/SYS.creds
2019/10/14 21:35:40 Connected to NATS Deployment: 127.0.0.1:4222
2019/10/14 21:35:40 No certificate file specified; using http.
2019/10/14 21:35:40 Prometheus exporter listening at http://0.0.0.0:7777/metrics
```

- using `--jwt` and `--seed` options to provide user JWT and NKey seed directly:

```sh
./nats-surveyor --jwt $NATS_USER_JWT --seed $NATS_NKEY_SEED
2019/10/14 21:35:40 Connected to NATS Deployment: 127.0.0.1:4222
2019/10/14 21:35:40 No certificate file specified; using http.
2019/10/14 21:35:40 Prometheus exporter listening at http://0.0.0.0:7777/metrics
```

## Config

### Config Files

Surveyor uses Viper to read configs, so it will support all file types that Viper supports (JSON, TOML, YAML, HCL, envfile, and Java properties)

To use a config file pass the `--config` flag. The defaults are `/etc/nats-surveyor/nats-surveyor[.ext]` and `./nats-surveyor[.ext]` with one of the supported extensions.

The config is simple, just set each flag in the config file. Example `nats-surveyor.yaml`:

```yaml
servers: nats://127.0.0.1:4222
accounts: true
log-level: debug
```

### Environment Variables

Environment variables are also taken into account. Any environment variable that is prefixed with `NATS_SURVEYOR_` will be read.

Each flag has a matching environment variable, flag names should be converted to uppercase and dashes replaced with underscores.  Example:

```
NATS_SURVEYOR_SERVERS=nats://127.0.0.1:4222
NATS_SURVEYOR_ACCOUNTS=true
NATS_SURVEYOR_LOG_LEVEL=debug
```

## Metrics

Scrape output is the in form of nats_core_NNNN_metric, where NNN is `server`, `route`, or `gateway`.

To aid filtering, each metric has labels.  These include `server_cluster`, `server_name`, and `server_id`.
Routes have the additional label `server_route_name` and gateways have the additional label `server_gateway_name`.

The info metrics has a nats_server_version label with the current version.

Additionally, there is a `nats_up` metric that will normally return 1, but will return 0
and no additional NATS metrics when there is no connectivity to the NATS system.  This
allows users to differentiate between a problem with the exporter itself connectivity with
the NATS system.

## Docker Compose

An easy way to start the NATS Surveyor stack (Grafana, Prometheus, and NATS
Surveyor) is through docker-compose.

Follow these links for installation instructions:

- [Docker Installation](https://docs.docker.com/v17.09/engine/installation/)
- [Docker Compose Installation](https://docs.docker.com/compose/install/)

### Environment Variables

The following environment variables MUST be set, either in your environment or
through the [.env](./docker-compose/.env) file that is automatically read by
docker-compose.  There is a `survey.sh` script that will set them for you as
a convenience.

| Environment Variable | Example | Description |
|--|--|--|
| NATS_SURVEYOR_SERVERS | nats://hostname:4222 | The URLs of any deployed NATS server(s) |
| NATS_SURVEYOR_CREDS   | ./SYS.creds          | NATS 2.0 System Account credentials |
| NATS_SURVEYOR_SERVER_COUNT | 9               | Number of expected NATS servers |
| PROMETHEUS_STORAGE    | ./storage/prometheus | Path to store prometheus data locally |
| SURVEYOR_DOCKER_TAG   | latest               | Surveyor docker tag to pull |
| PROMETHEUS_DOCKER_TAG | latest               | Prometheus docker tag to pull |
| GRAFANA_DOCKER_TAG    | latest               | Grafana docker tag to pull |

Note:  For referencing files and paths, docker always expects volume mounts
to be either a fully qualified directory, or a relative directory beginning
with with `./`.

#### Server URLs

You only need to connect to a single NATS server to monitor your entire NATS
deployment.  In configuring NATS_SURVEYOR_SERVERS, only one server is required,
but it's recommended you provide a list for backup servers to connect to, e.g.
`nats://host1:4222,nats://host2:5222`.  Valid urls are formatted as `hostname`
(defaulting to port 4222), `hostname:port`, or `nats://hostname:port`.

### Starting Up

You can start the Surveyor stack two ways.  The first is through docker
compose.  Ensure the environment varibles are set, that you are working
from the /docker-compose directory and run `docker-compose up`.

```bash
$ docker-compose up
Recreating nats-surveyor ... done
Recreating prometheus    ... done
Recreating grafana       ... done
Attaching to nats-surveyor, prometheus, grafana
...
```

Alternatively, you can pass variables into the `survey.sh` script in the
docker-compose directory.

```bash
$ ./survey.sh
usage: survey.sh <url> <server count> <system credentials>
```

e.g.

`./survey.sh  nats://mydeployment:4222 24 /privatekeys/SYS.creds`

If things aren't working, look in the output for any lines that contain
`exited with code 1` and address the problem. They are usually docker
volume mount problems or connectivity problems.

Next, with your browser, navigate to `http://127.0.0.1:3000`, or if you are
running the Surveyor stack remotely, the hostname of the host running the
NATS surveyor stack, e.g. `http://yourremotehost:3000`.

The first time you connect, you'll need to login:

- User:  *admin*
- Password: *admin*

After logging in, navigate to "Manage dashboards" and you'll see a dashboard
available named **NATS Surveyor**, where you'll be able to monitor your
entire NATS deployment.

### Stopping (while keeping the containers)

To stop the surveyor stack, but keep the containers run: `docker-compose stop`

### Restarting Surveyor

To restart the surveyor stack after being stopped, run: `docker-compose up`

### Stopping and removing containers

To cleanup your installation, run: `docker-compose down`

### Running Surveyor as a service

For platforms that support `systemd`, [surveyor.service](./docker-compose/surveyor.service)
is provided as a service definition template.  Modify and save this file as
`/etc/systemd/system/surveyor.service`.

`systemctl start surveyor` will launch the service.

### Errors

The logs should normally contain enough information about the cause of
problems or errors.

If you encounter a Prometheus error of:
`panic: Unable to create mmap-ed active query log`, set the UID of the
container to match the UID of your user in the
[docker-compose](./docker-compose/docker-compose.yml) file.

e.g:

```yaml
  prometheus:
    image: prom/prometheus:${PROMETHEUS_DOCKER_TAG}
    user: "1000:1000"
```

If the above doesn't work, using `root` will work but may pose a
security thread to the node it is running on.

```yaml
  prometheus:
    image: prom/prometheus:${PROMETHEUS_DOCKER_TAG}
    user: root
```

More information can be found [here](https://github.com/prometheus/prometheus/issues/5976).

## Service Observations

Services can be observed by creating JSON files in the `observations` directory.
The file extension must be `.json`.
Only one authentication method needs to be provided.
Example file format:

```json
{
  "name":       "my service",
  "topic":      "email.subscribe.>",
  "jwt":        "jwt portion of creds, must include seed also",
  "seed":       "seed portion of creds, must include jwt also",
  "credential": "/path/to/file.creds",
  "nkey":       "nkey seed",
  "token":      "token",
  "username":   "username, must include password also",
  "password":   "password, must include user also",
  "tls_ca":     "/path/to/ca.pem, defaults to surveyor's ca if one exists",
  "tls_cert":   "/path/to/cert.pem, defaults to surveyor's cert if one exists",
  "tls_key":    "/path/to/key.pem, defaults to surveyor's key if one exists"
}
```

Files are watched and updated using [fsnotify](https://github.com/fsnotify/fsnotify)

## JetStream

JetStream can be monitored on a per-account basis by creating JSON files in the `jetstream` directory.
The file extension must be `.json`.
Only one authentication method needs to be provided.
e sure that you give access to the `$JS.EVENT.>` subject to your user.
Example file format:

### Credentials

```json
{
  "name":       "my account",
  "jwt":        "jwt portion of creds, must include seed also",
  "seed":       "seed portion of creds, must include jwt also",
  "credential": "/path/to/file.creds",
  "nkey":       "nkey seed",
  "token":      "token",
  "username":   "username, must include password also",
  "password":   "password, must include user also",
  "tls_ca":     "/path/to/ca.pem, defaults to surveyor's ca if one exists",
  "tls_cert":   "/path/to/cert.pem, defaults to surveyor's cert if one exists",
  "tls_key":    "/path/to/key.pem, defaults to surveyor's key if one exists"
}
```

Files are watched and updated using [fsnotify](https://github.com/fsnotify/fsnotify)

## TODO

- [ ] Windows builds
- [ ] Other events (connections, disconnects, etc)
- [ ] Best Guess Server Count

[License-Url]: https://www.apache.org/licenses/LICENSE-2.0
[License-Image]: https://img.shields.io/badge/License-Apache2-blue.svg
[Build-Status-Image]: https://github.com/nats-io/nats-surveyor/workflows/Testing/badge.svg
[Build-Status-Url]: https://github.com/nats-io/nats-surveyor/workflows/Testing/badge.svg
[Coverage-Url]: https://codecov.io/gh/nats-io/nats-surveyor
[Coverage-image]: https://codecov.io/gh/nats-io/nats-surveyor/branch/main/graph/badge.svg
