# fall-guys

Network replacement for **Fall Guys** on Nintendo Switch.

## Status (2026-10-24): reopened, partly feasible

The 2026-09 verdict said "not feasible, documentation only". It is revised after the Switch
client's own code was read ([`docs/protocol.md`](docs/protocol.md)):

- **Buildable:** everything up to the match server. That means EOS sign-in (in
  [`../eos`](../eos)), the Catapult login and gateway (profile, inventory, store, shows,
  matchmaking), and an authored content document. The client describes every wire shape it
  uses.
- **Not buildable from the client:** the rounds themselves. The shared game code ships the
  per-obstacle server reactions, but the round authority (spawning, qualification, elimination,
  scoring, show flow) is an interface that nothing in the build implements, and there is no
  client-hosted mode. A round server would have to be written from scratch against the
  protocol, one round type at a time. Full parity with Epic's servers is not a realistic goal.

Code so far: `cmd/catapultd`, the login service (`POST /api/v1/login`, `EosSignIn` tokens
checked against eosd's JWKS). It is not deployed: the Catapult host names are outside the
signed redirect ceiling until the owner adds `.oncatapult.com`.

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
