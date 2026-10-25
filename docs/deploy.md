# Deploy: fall-guys

Facts from the `Dockerfile`, `cmd/catapultd/main.go` and `cmd/roundd/main.go`.

## Build

The `Dockerfile` builds both binaries from `./cmd/catapultd` and `./cmd/roundd` into a
`gcr.io/distroless/static` image that runs as `nonroot`. The default entrypoint is `/catapultd`;
run the same image with `--entrypoint /roundd` for the round server.

Plain HTTP runs inside the pod network. Traefik terminates the console's TLS for the
`oncatapult.com` names and forwards the stream.

## Ports

| Port | Protocol | Process | Setting |
| --- | --- | --- | --- |
| `23530` | TCP | `catapultd` (login, gateway, content, internal match endpoint) | `CATAPULT_ADDR` |
| `23533` | UDP | `roundd` (Hazel round transport) | `ROUND_ADDR` |

## Environment

| Variable | Process | Default | Meaning |
| --- | --- | --- | --- |
| `CATAPULT_SECRET` | both | none, required | Signs session tokens and the round server's entry tokens |
| `CATAPULT_ADDR` | `catapultd` | `:23530` | Listener |
| `EOSD_JWKS_URL` | `catapultd` | `http://127.0.0.1:22620/auth/v1/oauth/jwks` | eosd's published keys, used to check EOS Auth tokens |
| `CATAPULT_CONTENT_BASE` | `catapultd` | `http://login.fallguys.oncatapult.com` | Base of the content (CMS) document URL |
| `CATAPULT_CONTENT_VERSION` | `catapultd` | `internal/cms` version | Content document version |
| `CATAPULT_MMS_URL` | `catapultd` | `wss://gateway.fallguys.oncatapult.com/mms` | Matchmaking socket URL |
| `CATAPULT_MIN_PLAYERS` | `catapultd` | `2` | Players needed to start a match |
| `CATAPULT_MAX_WAIT` | `catapultd` | `30s` | How long matchmaking waits |
| `ROUND_SERVER_IP` | `catapultd` | `127.0.0.1` | Round server address handed to clients |
| `ROUND_SERVER_PORT` | `catapultd` | `23533` | Round server port handed to clients |
| `ROUND_ADDR` | `roundd` | `:23533` | Round server UDP listener |
| `ROUND_LEVEL` | `roundd` | `round.DefaultLevel` | Round to run; must be a known level |
| `CATAPULT_INTERNAL_URL` | `roundd` | `http://127.0.0.1:23530` | `catapultd` base for match lookups |
