# Notes: fall-guys

## Overview

Network replacement for **Fall Guys** on Nintendo Switch.

## Architecture

The rounds are the hard part. The client ships the per-obstacle server reactions but none of
the round authority (spawning, qualification, elimination, scoring, show flow), and it has no
client-hosted mode. So `roundd` is written from scratch against the protocol, one round type
at a time. Full parity with Epic's servers is not the goal; each round type below is added
and tested on its own.

| Piece | Code |
|---|---|
| EOS sign-in, EOS Auth (Epic account) | [`../eos`](../../eos) |
| Catapult login (privacy policy, EULA, login) | `cmd/catapultd` |
| Gateway (profile, wallet, settings, matchmaking token) | `cmd/catapultd` |
| Content document (one show, one round) | `internal/cms` (served over plain http: the downloader rejects the OpenPak CA) |
| Matchmaking socket and match status | `cmd/catapultd` |
| Round server transport (Hazel, HLAPI framing) | `internal/hazel`, `internal/hlapi` |
| Round server flow (connect, load, spawn, start, end) | `internal/round`, `cmd/roundd` |

Round types: race (reach the finish), survival, team, hunt, logic, final. Each is complete when two
accounts on Harbor play it to the results screen.

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
