# fall-guys

> **Superseded 2026-09-24 by [`servers/eos`](../eos).** The study below is carried there as
> `docs/fall-guys.md` and `docs/fall-guys-evidence.md`. Its identity-layer verdict is
> reversed in principle (OpenPak answers `api.epicgames.dev` itself); its match-server
> verdict stands. Nothing here is maintained.

Feasibility study for replacing the network **Fall Guys** uses on Nintendo Switch.

## Status: not feasible. Documentation only.

There is no code here and none is planned. The full reasoning is in
[`docs/design.md`](docs/design.md); the short version:

Fall Guys on Switch is an Epic live service with three third-party backends —
Epic Online Services for identity (`api.epicgames.dev`), Mediatonic's Catapult
backend for login and matchmaking, and Epic-hosted **authoritative match servers
that run the game simulation**. The first is a token Epic verifies with Nintendo,
so an OpenPak identity can never satisfy it without us replacing EOS. The third
is the wall: up to 60 players' physics, level state and round outcomes are
computed on a server build that has never shipped to anyone, and Fall Guys has no
client-hosted or listen-server mode. Replacing it means writing the game, not the
network.

That is unlike [`servers/outbound`](../outbound), where Photon Fusion's Shared
mode puts the simulation inside the client and [`servers/photon`](../photon) can
host the game's own `Simulation.Server`. No equivalent exists here.

The service is also still live, so this is not a preservation case. Fall Guys
breaks on an OpenPak console only because the console's identity is ours. The
right handling in production is to **not** redirect `api.epicgames.dev` or
`*.oncatapult.com` and let the title reach the real service.

Consequences:

- No port block is claimed in [`ports.md`](https://docs.openpak.org/contributors/ports/).
- No deploy target, compose entry or status check.
- [`docs/evidence-needed.md`](docs/evidence-needed.md) lists the experiments that
  would confirm or overturn this, cheapest first. Only the first one (does the
  Switch build contain a client-hosted match path?) can reopen the project.

Reopen this repository if a server build becomes obtainable, if someone ships an
open-source authoritative Fall Guys server under an AGPL-3.0-compatible licence,
or if that first experiment finds a client-hosted path.

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
