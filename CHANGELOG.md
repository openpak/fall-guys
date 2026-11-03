# Changelog — fall-guys

Generated from git history on 2026-09-15. `git log` stays the source
of truth; this file is the readable summary.

## v0.2.0 — 2026-11-03

- Rewards: `roundd` reports `game_sessions.fall-guys` from who is connected to the round server;
  `catapultd` maps accounts to BAAS user ids through eosd. Account map and session clock live in
  Postgres (`FALL_GUYS_DATABASE_URL`); restart tests against a real Postgres
  (`FALL_GUYS_TEST_DATABASE_URL`).

## v0.1.0 — 2026-11-02

- Release workflow publishing to ghcr.io; replaces the hand-built production image.

- Add a release workflow publishing the image to ghcr.io on v*.*.* tags.

## History — no releases tagged yet

- chore: drop the empty module; go test ./... exits 1 with no packages [45cc512]
- docs: Fall Guys on Switch is not feasible; verdict, evidence plan, no code [d64dde3]
- seed: empty fall-guys, AGPL-3.0-only, clean-room gate [c2e3ea5]

