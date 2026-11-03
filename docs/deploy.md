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

## Rewards sessions and durable state

`roundd` reports `game_sessions.fall-guys`: a player counts once per match after 3 continuous minutes
connected to the round server together with another player (a match whose round ended is no longer a
room). `catapultd` maps the Epic account to a BAAS user id at login (eosd `GET /internal/v1/owner/<id>`)
and `roundd` reads that back from `catapultd` `/internal/v1/baas`. Accounts eosd never issued are left
out and never count.

| Variable | Process | Meaning |
| --- | --- | --- |
| `FALL_GUYS_DATABASE_URL` | both | Postgres (`postgres://fall_guys:...@openpak-postgres:5432/fall_guys?sslmode=disable`): `fg_accounts` (account -> BAAS id) and `fg_session_clock`. Pool of 2. Down: runs on memory, ERROR log, retried every 30 s. Unset: memory only (development) |
| `EOS_INTERNAL_URL`, `EOS_INTERNAL_KEY` | `catapultd` | eosd health listener (`http://openpak-eos:22621`) and its internal key (eos >= v0.5.0) |
| `REWARDS_EVENTS_URL`, `REWARDS_EVENTS_KEY` | `roundd` | rewards (bridge `http://openpak-rewards:20180`); unset: no events |
| `REWARDS_SESSION_MIN` | `roundd` | optional, default 3m |

Matchmaking's live queue and in-flight matches are not persisted: they exist only while the round
server's UDP connections do.
