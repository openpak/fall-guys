# fall-guys

Network replacement for **Fall Guys** on Nintendo Switch.

## Status (2026-10-24): lobby layer built, round server started

The rounds are the hard part. The client ships the per-obstacle server reactions but none of
the round authority (spawning, qualification, elimination, scoring, show flow), and it has no
client-hosted mode. So `roundd` is written from scratch against the protocol, one round type
at a time. Full parity with Epic's servers is not the goal; each round type below is added
and tested on its own.

| Piece | Code | State |
|---|---|---|
| EOS sign-in, EOS Auth (Epic account) | [`../eos`](../eos) | works on Harbor |
| Catapult login (privacy policy, EULA, login) | `cmd/catapultd` | works on Harbor |
| Gateway (profile, wallet, settings, matchmaking token) | `cmd/catapultd` | written; more handlers follow the client's requests |
| Content document (one show, one round) | `internal/cms` | authored; served over plain http (the downloader rejects the OpenPak CA), route pending |
| Matchmaking socket and match status | `cmd/catapultd` | written |
| Round server transport (Hazel, HLAPI framing) | `internal/hazel`, `internal/hlapi` | written, unit tested |
| Round server flow (connect, load, spawn, start, end) | `internal/round`, `cmd/roundd` | written, unit tested |

### Round types

Each round type is done when two accounts on Harbor play it to the results screen.

| Round type | Example | State |
|---|---|---|
| Race (reach the finish) | first race round | in progress: flow written; level data, movement relay and two-account Harbor test to do |
| Survival | | not started |
| Team | | not started |
| Hunt | | not started |
| Logic | | not started |
| Final | | not started |

## Ground rules

- Clean room. Read [clean-room policy](https://docs.openpak.org/contributors/clean-room/)
  before writing a line. Public protocol documentation, our own captures, and
  compatibly licensed code are the only inputs. Provenance for this study is
  recorded at the end of `docs/design.md`.
- AGPL-3.0-only, English only, Go 1.25, no secrets in the repository.

This project is not affiliated with or endorsed by Nintendo, Mediatonic, Epic
Games, or any original service provider. It contains no proprietary game code,
assets, SDK material, binaries, certificates, keys, firmware, extracted files,
credentials, or raw packet captures. Fall Guys is a trademark of Mediatonic
Limited.
