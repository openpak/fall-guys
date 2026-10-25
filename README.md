# Fall Guys

OpenPak's Fall Guys Server.

| | |
| --- | --- |
| Game | [Fall Guys](https://openpak.org/titles#t-21) |
| Network | Nintendo Switch |
| Licence | AGPL-3.0-only |

## What it serves

| Host | What | Served by |
| --- | --- | --- |
| `login.fallguys.oncatapult.com` | Login (`/api/v1/`), content document | `catapultd` |
| `gateway.fallguys.oncatapult.com` | Gateway socket: profile, wallet, settings, matchmaking | `catapultd` |
| UDP `23533`, address from the match status | Rounds (Hazel transport) | `roundd` |

## Run

```sh
go build -o catapultd ./cmd/catapultd
go build -o roundd ./cmd/roundd
CATAPULT_SECRET=<secret> ./catapultd
CATAPULT_SECRET=<secret> ./roundd
```

## Docs

- [Protocol](docs/protocol.md)
- [Evidence](docs/evidence.md)
- [Deploy](docs/deploy.md)
- [Notes](docs/notes.md)
- [Design](docs/design.md)
- [Evidence still needed](docs/evidence-needed.md)

Live compatibility status: [openpak.org/titles#t-21](https://openpak.org/titles#t-21).

## Licence

AGPL-3.0-only. See [LICENSE](LICENSE).
