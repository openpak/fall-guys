# Evidence needed

The verdict in [`design.md`](design.md) is **not feasible**. This list is what
would confirm or kill it, cheapest and most decisive first. Stop as soon as one
experiment confirms the verdict — only E1 can overturn it outright.

Read `servers/shared-docs/online-compatibility-playbook.md` (layered model,
experiment discipline) and `emulator-launch-protocol.md` before touching an
emulator. Every rule there applies here, in particular:

- One variable per run; a failed experiment is a result.
- Raw dumps, captures, Ghidra projects and logs stay **out of this repository**
  and off `/tmp` (tmpfs, 15 GB, and filling it kills the shell tool). Disk-backed
  scratch only.
- One citron instance per profile; its log truncates on every start.

Tooling already present in `Openpak/emulators/citron`: exact-host DNS redirect
`OPENPAK_FALLGUYS_EOS_IP` (+ `OPENPAK_FALLGUYS_EOS_PORT` to move the
destination off 443), `OPENPAK_LOG_EOSSDK_BASE` for the runtime base of the
`nn::ro`-loaded EOS SDK module, and `OPENPAK_BAAS_SUB` to pin the BAAS
`id_token` subject. All default-off. Log:
`~/.local/share/citron/log/citron_log.txt`.

---

## E1 — Does the Switch build contain any client-hosted match path?

**The only experiment that can reopen the project.** Everything else refines a
verdict that E3/E5 already decide.

- **Method.** From our own dump of the installed title (analysis off-repo, on the
  pool): convert the active-update NSO to ELF (`nx2elf`), then string- and
  symbol-scan for a server-side entry point — `DedicatedServer`, `IsServer`,
  `StartHost`, `-batchmode`/`-nographics` style headless switches, GameLift or
  Multiplay SDK strings, and any UNet/Mirror/ENet server symbols. The IL2CPP
  metadata reading method in
  [djkaty's write-up](https://katyscode.wordpress.com/2020/08/10/practical-il2cpp-protobuf/)
  gives class and method names without a full Ghidra pass.
- **Decision rule.** A reachable listen-server or headless-simulation path →
  **verdict reopens**, and the project becomes "host the client's own simulation",
  the shape `servers/photon` already proved for Outbound. Only client-side
  prediction and replication of server-owned state → **verdict confirmed, stop.**
- **Cost.** Hours. No live service contact.

## E2 — Endpoint inventory for the current Switch build

- **Method.** Boot Fall Guys under citron with the family redirect hooks
  **unset**, let it reach the sign-in screen, and mine the DNS flow from
  `citron_log.txt` (`sfdnsres` resolution lines). Record every hostname in order.
- **Answers.** Whether the 2026 Switch build still uses `*.oncatapult.com`
  (currently marked *unknown* in the design), what CDN/telemetry hosts appear,
  and whether anything Nintendo-side is on the critical path.
- **Decision rule.** Facts only — this cannot flip the verdict, it scopes layer 2
  if E1 reopens it. Skip unless E1 reopens.
- **Cost.** One run.

## E3 — Where exactly does EOS Connect abort?

- **Method.** Point `OPENPAK_FALLGUYS_EOS_IP` at a local terminator that logs
  metadata only (TLS handshake parameters, request line, header names, byte
  counts — never values), with `OPENPAK_FALLGUYS_EOS_PORT` if 443 is taken.
  Run twice: once with the default random BAAS `sub`, once with
  `OPENPAK_BAAS_SUB` pinned to the linked account's uid (the 2026-08-31
  hypothesis was a cross-check against `IManagerForApplication::GetAccountId`).
  Use `OPENPAK_LOG_EOSSDK_BASE=1` to translate SDK-relative offsets if the
  failure needs to be located in the module.
- **Decision rule.** The client never sends the external-auth request →
  the gate is client-local and layer 1 requires patching a shipped third-party
  SDK per build; **verdict confirmed at high confidence.** The client sends and
  waits → layer 1 is at least *addressable* by a server, and the verdict still
  stands on layer 3 but the cost estimate improves.
- **Cost.** Two runs plus a throwaway terminator. Do not reuse an OpenPak
  production listener.

## E4 — What trust material does the EOS SDK carry?

- **Method.** Same redirect as E3, self-signed certificate for
  `api.epicgames.dev`. Record whether the client fails at handshake or after it.
- **Decision rule.** Handshake refused → the SDK carries its own CA store or
  pins, and every EOS replacement needs a binary patch on top of everything else.
  Handshake accepted → console/emulator trust is enough, one obstacle less.
- **Cost.** One run. Only worth doing if E3 shows the client speaks at all.

## E5 — Catapult and match-server shape for a current build

**Only if E1 reopened the project.** Not worth doing otherwise.

- **Method.** Recover the protobuf-net schema from the current Switch build with
  the IL2CPP extraction method above, and compare against the 2020-era MIT
  structures in [`klukule/FallGuys.Protocol`](https://github.com/klukule/FallGuys.Protocol)
  to see how far the protocol has moved. For the match-server handoff, observe
  which address and transport the client is handed after matchmaking.
- **Caution.** Observing a real match means contacting Epic's live service with a
  real account. That is a terms-of-service question, not a technical one; get a
  decision before doing it, and never point a modified client at production.
- **Decision rule.** Recovering the schema is necessary for a Catapult
  replacement and sufficient for nothing.

---

## What would be watched instead of captured

The two reopen conditions that no capture of ours can produce, tracked passively:

- An open-source authoritative Fall Guys server appearing under an AGPL-3.0-
  compatible licence. GitHub topics [`fallguys`](https://github.com/topics/fallguys)
  and [`fall-guys`](https://github.com/topics/fall-guys) held none as of
  2026-09-09.
- A shutdown announcement plus a preserved server build.
