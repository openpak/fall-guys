# Fall Guys on Nintendo Switch — network stack and feasibility

Status: **research verdict, 2026-09-09. No implementation, and none planned.**
Author: tobagin. Clean room: no earlier source was read for this document; see
[Provenance](#provenance).

## Verdict

**Not feasible. OpenPak should not build a Fall Guys network.**

Fall Guys is an Epic-operated live service whose gameplay is computed on
Epic-hosted authoritative servers running a Unity build that ships to nobody.
Replacing the network means writing the game simulation. That is a game
reimplementation, not a network service, and it is outside what OpenPak does.

This repository therefore stays documentation-only: no Go code, no port block,
no deploy target. The reopen conditions are at the end.

## What the Switch build talks to

Fall Guys reached Switch on 21 June 2022, free-to-play, *after* Epic acquired
Mediatonic — the Switch has never had a pre-Epic build ([TechCrunch][tc]). So the
Switch client is the modern, EOS-gated one from day one, and there is no legacy
version of it to target.

Three distinct backends, all third party, none of them Nintendo:

| # | Layer | Endpoint | Transport | Who runs it |
|---|---|---|---|---|
| 1 | Identity | `api.epicgames.dev` (CONFIRMED, see Provenance) | HTTPS, EOS SDK | Epic Online Services |
| 2 | Game backend ("Catapult") | `login.fallguys.oncatapult.com`, `gateway.fallguys.oncatapult.com` (PC-observed; **unknown on Switch, needs E2**) | WSS, protobuf-net messages | Mediatonic/Epic |
| 3 | Match servers | assigned at matchmaking, address **unknown, needs E5** | UDP; UNet-derived on 2020 builds, current transport unknown | Epic-hosted dedicated servers |

Nintendo's own services (BAAS, NEX, NPLN) carry **none** of this. NEX is not
involved at all. The only Nintendo involvement is the identity token the console
hands the EOS SDK at sign-in, which is exactly where OpenPak breaks the game.

### 1. Identity — Epic Online Services

The client embeds the EOS SDK as a runtime-loaded module
(`EOSSDK-Switch-Shipping.nrs`; the sibling Among Us Switch client ships the same
one) and signs in through the **Connect interface**, which exchanges an external
identity for an EOS Product User ID ([Connect reference][eosconnect]). On Switch
the external identity is a Nintendo token — an NSA ID token or a Nintendo Account
ID token, both minted by Nintendo and, per Epic's identity-provider
documentation, verified by Epic's backend against the configured provider
([Identity Provider Management][eosidp]). Epic surfaces the failure path to
players as "EoS Login Error 200_001" on Switch ([Epic support][eos200]).

This is the same wall Outbound hit with Photon's built-in `NintendoSwitch`
custom-auth provider: a token OpenPak mints is not a token Nintendo minted, and
the third party validates it upstream. We cannot make Epic accept it, so
replacing layer 1 means standing up our own EOS: `api.epicgames.dev` redirected
to us, plus every EOS service the title actually uses (Connect, and unknown
amounts of Sessions/Lobbies/P2P/Stats/AntiCheat), plus defeating whatever TLS
trust the shipped SDK carries.

Worse, under emulation the exchange **fails client-side regardless of what the
server answers** (observed 2026-08-31, recorded in the citron tree — see
Provenance). Whatever we serve on `api.epicgames.dev`, the SDK currently aborts
locally. Fixing that means patching a shipped, obfuscated third-party SDK per
build. Cost: high. Fatal on its own: no. Fatal in combination: yes.

### 2. Catapult — Mediatonic's own backend

Public prior art exists for this layer and only this layer, all MIT:

- [`klukule/FallGuys.Protocol`][proto] — protobuf-net message structures, "used
  for most of the communication between player and server".
- [`klukule/CatapultServer.Proxy`][proxy] — a MITM proxy for the login and
  gateway WebSocket streams; names `*.fallguys.oncatapult.com` and its
  `-prod`/dev/staging branches. Explicitly "NOT CUSTOM SERVER".
- [`repinek/fallguys-server-changer`][changer] — Frida/il2cpp URI redirection,
  with a version table covering 0.9.10 through 21.1.1 and the note that login
  became a WebSocket at 10.0.0. Explicitly not a server.
- [djkaty's IL2CPP protobuf-net extraction write-up][katy] and
  [`Il2CppProtoExtractor-FallGuys`][katyrepo] — the method for recovering the
  message schema from a current build.

So the message shapes are recoverable and a Go Catapult replacement is real
work but ordinary work: a WSS endpoint, protobuf-net framing, a login flow, a
profile/cosmetics store, matchmaking that hands out a match-server address.

It buys nothing on its own. A player who logs in and reaches the lobby with no
server to play a round on has not been given Fall Guys.

### 3. Match servers — the wall

Fall Guys rounds are simulated server-side: up to 60 players' physics, level
state, eliminations and round outcomes are computed by a dedicated server, not
by any client ([MPG case study][mpg], [Wikipedia][wiki]). The 2020-era protocol
work says exactly this in passing — its protobuf schema covers everything
"except for UNet based gameservers themself" ([FallGuys.Protocol][proto]).

That build is Epic's. It has never shipped to players on any platform. There is
no listen-server or host-migration mode; "Custom Shows" are private *lobbies* on
the same hosted infrastructure, not player-hosted servers. A survey of GitHub's
[`fallguys`][topic1] and [`fall-guys`][topic2] topics turns up mod menus, CMS
decryptors, level tools, a traffic redirector and a protocol dump — and no
server emulator, open or otherwise. The scene's "private servers" referred to by
the redirector are closed and, on the evidence available, not playing real
rounds.

**Contrast with the two OpenPak titles that do work.** Outbound is Photon Fusion
in *Shared* mode: the simulation ships inside the client, so
[`servers/photon`](../../photon) can host Fusion's own `Simulation.Server` taken
from the game's own build and two players share a world. Stardew is NPLN
brokering with the simulation on a player's console. Both are cases where
*somebody who is not the publisher already has the simulation*. Fall Guys is the
opposite case, and it is the one that cannot be solved by writing a server:
matching the client's expectations round by round — every obstacle's physics
tuning, every elimination rule, every level in every season — is rebuilding the
game.

## Why this is not a preservation case either

The service is live. Nothing has been shut down. A retail Switch with a real
Nintendo account plays Fall Guys today with no help from us.

The game is only broken *because of OpenPak*: a console pointed at our stack has
an OpenPak identity, and Epic will not accept a Nintendo token we minted. The
correct and cheapest handling is therefore to leave it alone — do not redirect
`api.epicgames.dev` or `*.oncatapult.com` on a production OpenPak console, and
let Fall Guys reach the real service. That does not make it work on an
OpenPak-only account, but nothing we could build in this repository would
either, short of the three layers above.

## Scope decision

Nothing is in scope. Specifically:

- **No port block claimed** in [`ports.md`](../../../ports.md). The
  22600–22699 third-party range keeps its ten free for a title that can use them.
- **No Go code.** A Catapult skeleton would be a login server for a game with no
  rounds; an EOS stub would be a stub the client refuses to talk to. Both would
  be scaffolding that looks like progress.
- **No deploy path, no compose entry, no status-page check.**

If someone insists on a partial build anyway, the honest framing is: layer 2 is
maybe two weeks of work and produces a lobby, layer 1 is an open-ended fight with
a shipped SDK, and layer 3 is not a project.

## Reopen conditions

Revisit only if one of these becomes true, and prefer the evidence in
[`evidence-needed.md`](evidence-needed.md) over re-deriving this document:

1. **A server build becomes obtainable** — Epic ships or leaks a headless Fall
   Guys server, or the game is shut down and preserved with one. This has not
   happened for any Epic-operated title.
2. **Someone ships an open-source authoritative Fall Guys server** that actually
   plays rounds, under a licence compatible with AGPL-3.0-only. Then OpenPak's
   job shrinks to layers 1 and 2 and this verdict flips to "hard but real".
3. **E1 finds a client-hosted path** in the Switch build (a listen-server or
   headless mode). Unlikely — the architecture rules it out — but it is the one
   cheap check that could overturn the whole verdict, so it is E1.

## Provenance

Public sources are linked inline. Two categories of non-public input were used,
both permitted by [`clean-room-policy.md`](../../../docs/clean-room-policy.md)
as observed console behaviour and our own repositories:

- **`Openpak/emulators/citron`** (our own emulator tree, GPL-derived, readable
  under the policy). Facts taken, no code copied: Fall Guys' confirmed EOS
  bootstrap hostname is `api.epicgames.dev`; the EOS SDK is loaded at runtime as
  a separate module; and a note dated 2026-08-31 records that Fall Guys' EOS
  Connect external-auth exchange fails client-side regardless of the server's
  response. Redirect hooks for this title already exist there
  (`OPENPAK_FALLGUYS_EOS_IP`, `OPENPAK_FALLGUYS_EOS_PORT`,
  `OPENPAK_LOG_EOSSDK_BASE`).
- **`Openpak/docs/playbooks/online-compatibility-playbook.md`** for method.
  Note that it cites a "Fall Guys/EOS" worked example whose handoff lives in a
  **source-available tree that must not be read**; it was not opened, and nothing
  in this document comes from it. If that prior work contains a contradicting
  result, it will have to be re-derived by capture, not consulted.

No proprietary binary, dump, capture, token or key is stored in this repository.

[tc]: https://techcrunch.com/2022/05/16/fall-guys-free-to-play-coming-to-switch-xbox-june-21
[eosconnect]: https://dev.epicgames.com/docs/game-services/connect/connect-reference
[eosidp]: https://dev.epicgames.com/docs/epic-online-services/eos-fundamentals/identity-provider-management
[eos200]: https://www.epicgames.com/help/en-US/c-Category_FallGuys/c-FallGuys_TechnicalSupport/how-to-solve-eos-login-error-200-001-on-nintendo-switch-a000086689
[proto]: https://github.com/klukule/FallGuys.Protocol
[proxy]: https://github.com/klukule/CatapultServer.Proxy
[changer]: https://github.com/repinek/fallguys-server-changer
[katy]: https://katyscode.wordpress.com/2020/08/10/practical-il2cpp-protobuf/
[katyrepo]: https://github.com/djkaty/Il2CppProtoExtractor-FallGuys
[mpg]: https://www.themultiplayergroup.com/case-studies/fall-guys
[wiki]: https://en.wikipedia.org/wiki/Fall_Guys
[topic1]: https://github.com/topics/fallguys
[topic2]: https://github.com/topics/fall-guys
