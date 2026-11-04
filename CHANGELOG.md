# Changelog — fall-guys

Generated from git history on 2026-09-15. `git log` stays the source
of truth; this file is the readable summary.

## v0.3.0 — 2026-11-04

- Account erasure (prds#120): `DELETE /internal/accounts/{account_id}/data` on a separate listener of catapultd (`ERASE_ADDR`, default `:9099`, not routed; `Authorization: Bearer $ACCOUNT_ERASE_KEY`, unset = 503). Looks up the account's BAAS ids in the core (`ACCOUNT_GRPC_ADDR`, `ACCOUNT_INTERNAL_KEY`, read-only ListAccountLinks; unconfigured = 500) and deletes the `fg_accounts` rows (Fall Guys account -> BAAS id), the account's entries in `fg_session_clock`, and catapultd's in-memory id cache, matchmaking queue entries and latest match for it. Nothing else is stored per player.

## v0.2.1 — 2026-11-03

- Rewards: the session room is the whole match, not one round. Presence is kept for 2 minutes after a
  drop, so a show of short rounds adds up to one session per player (test: four 50 s rounds count once
  per player, one round counts nobody).

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

