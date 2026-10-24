# Fall Guys (Switch): what the client speaks

Measured from the Switch client 11.6.1 (the owner's own dump, read statically) on 2026-10-24.
Facts only: host names, paths, property names and message names. Nothing here was copied from
any Epic or Mediatonic server. The private research note has the method and the evidence.

## Hosts

| Purpose | Host | Transport |
|---|---|---|
| Identity | `api.epicgames.dev` | EOS SDK over HTTPS. Served by `platforms/switch/games/eos` |
| Parental settings | `api.kws.ol.epicgames.com` | HTTPS |
| Login | `login.fallguys.oncatapult.com`, base path `/api/v1/` | HTTPS, JSON |
| Gateway | `gateway.fallguys.oncatapult.com` | WebSocket over TLS, protobuf-net |
| Analytics | `analytics-gateway.fallguys.oncatapult.com` | WebSocket over TLS |
| Content (CMS) | whatever `contentUrl` in the login answer names | HTTP(S) download |
| Match server | `IpAddress`/`Port` in the gateway's match status | UDP (Hazel, optional DTLS) |

## Sign-in chain

1. EOS sign-in with the console's Nintendo NSA id_token. This is an EOS **Auth** (Epic
   account) login, not only Connect.
2. `POST /api/v1/login` with `type: "EosSignIn"` and `token` set to the EOS Auth access token.
3. Download the content document from `contentUrl`.
4. Open the gateway socket.

## Login service

Paths under `/api/v1/`: `login`, `refresh-content`, `account`, `lookup/account`, `identity`,
`identity/epic`, `social-ban-status`.

`login` request: `type`, `token`, `properties`, `userParameters`, `clientVersion`,
`clientVersionSignature`, `platform`, `contentBranch`.

`login` answer: `token`, `expiry`, `accountId`, `identities`, `roles`, `contentUrl`,
`contentVersion`, `contentEtag`, `additionalProperties`, `tokenType`, `clientFeatures`,
`socialBans`.

Errors: `{title, errorCode, detail}`.

## Gateway envelope

protobuf-net. A frame is a `Message` (`Catapult_Message_V1`) whose concrete subtype
(`Message<T>`, `Result<T>`, `ErrorMessage`, ...) is a sub-message under a per-type field
number fixed by the client's generated serializer. Headers ride in field 1 the same way, as
`MessageHeader` subtypes (Operation, Auth, Correlation, Trace, Timestamp, RequestTime,
DateTimeOverride, UserSession). Incoming batches arrive as `Result<MessageBatch>`.

The subtype numbers and DTO field tags are tabulated in the private research notes. They go
into a generated codec, not into this document.

## Content (CMS)

- The login answer names the document (`contentUrl`) and its version (`contentVersion`). The
  client fetches the URL with a plain GET and `Accept-Encoding: gzip`. It gunzips the body
  itself, and only when the answer carries `Content-Encoding: gzip`. It then reads the body as
  UTF-8 JSON.
- On disk it caches the document as `content_v2/<version>.gdata`, XORed with a key the client
  holds. The XOR applies to the cache only, never to the wire. A new version replaces the
  cached copy.
- Document: one JSON object. `_meta._content_version` must be set. There is one key per table,
  151 tables in 11.6.1 (`internal/cms/tables.go`). Each table is an array of rows, and every
  row has a string `id`.
- **Every table key must be present.** The parser looks each one up directly, so a missing key
  fails the whole document. A `null` or an empty array gives an empty table.
- A field that refers to another row holds that row's id. A `table.id` form also works,
  because the client keeps what follows the first dot. Nested structures are objects.
- Enums are written as member names, matched without regard to case or `_`. Dates are ISO 8601
  strings or epoch seconds. Integers must be JSON integers, and booleans JSON booleans.
- `settings_global` rows are `{id, value}`. The ids are fixed by the client (for example
  `hazel_packet_sequencing`, default `true`, and `hazel_disconnect_timeout`).
- Round scenes are addressable keys shipped in the build (for example `FallGuy_DoorDash`),
  named by `prime_levels.scene_name`.

catapultd serves the authored document at `/content/<version>`.

## Match server

UNet HLAPI messages over a Hazel transport. The game's own messages are:

- From the server: `ConnectInitial`, `StartLoadingLevel`, `SpawnObject`, `UnspawnObject`,
  `StartGame`, `ObjectTransform(Compact)`, `MotorAgentState`, `MotorTasks`, `PlayerProgress`,
  `QualificationProgressUpdated`, `RoundResults`, `EndRound`, `ExecuteRPC`, `EventGeneric`,
  and others.
- From the client: `ConnectInitial`, `ReadyRound`, `SetReady`, `RequestSpawnPlayer`,
  `MotorTasks`, `ObjectTransform`, `RequestGeneric`, `ResetToCheckpoint`, `SkipRound`,
  `DisconnectPlayer`, `Ping`/`Pong`.

The client implements none of the server's round logic. See the README.
