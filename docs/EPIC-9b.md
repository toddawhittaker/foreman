# Epic 9b: VPN sidecars

Planning draft, 2026-11-14. Implementation is not approved or started.
Epic branch: `epic/9b-vpn-sidecars`. Base:
`fae69ca4aa5d37c96a697d5e7db7ef583eafc3fd` (`origin/main`).
Serves SPEC 3.8, 4.3, 4.4, 4.13, 12.5, 12.6, 14.3, 14.16, 18.9b,
and ADR 0006. Follow WORKFLOW and the epic-run skill for execution.

## Outcome

An operator supplies a running VPN sidecar. Foreman runs the game in its
network namespace. A LAN player and a remote VPN player can play together;
Foreman retains console, player tracking, graceful save/stop, restart and
world switching. Tailscale is the reference integration because Todd uses it.
The application remains VPN-provider independent.

Foreman does not install, authenticate, configure, restart or remove the
sidecar. No VPN credentials enter Foreman. No new dependency, database
migration, Docker endpoint or public API is planned. Epics 10 and 11 and
the deferred Gate B remain separate. Open issues checked on 2026-11-14:
none concern sidecars; this epic closes none of them.

## Decisions and scope

1. **LAN and VPN together:** the sidecar publishes `34197:34197/udp` on
   the host; the game publishes no ports and listens on all interfaces.
   Omit both `ExposedPorts` and `PortBindings` from the game container in
   shared-network mode. Ordinary mode keeps its current behavior.
2. **Dedicated sidecar (Todd, 2026-11-14):** require a dedicated VPN
   sidecar attached only to `FOREMAN_GAME_NETWORK`. Refuse a start when
   Docker reports any additional network attachment, with a plain reason
   and guidance to use a dedicated sidecar. T0 updates the current spec,
   which permits multiple Docker networks. This restriction concerns
   Docker attachments; the sidecar still provides its VPN interface and
   publishes the LAN game port. It does not replace VPN access rules.
3. **Identity:** resolve the configured name at every start, inspect that
   container, and join `container:<verified ID>`. Never silently follow a
   replacement between inspection and creation. A failed create/start
   reports the failure without falling back to another namespace.
4. **RCON:** bind only the sidecar's game-network address on TCP 27015.
   Pass the address through a game environment variable, not interpolated
   shell text. Keep one address-resolution path for initial connection,
   reconnect, and monitor reattachment after a Foreman restart.
5. **Lifecycle:** keep SPEC 4.13's manual recovery rule. No sidecar watcher,
   new restart warning or automatic restart in this epic. After restarting
   or replacing the sidecar, the operator restarts the game in Foreman.
   Test and document the actual behavior, including unreachable RCON.
6. **Warnings:** missing host publication of `34197/udp` is a nonblocking
   Server-page warning. A published port does not prove LAN reachability:
   document host-address binding, changed host ports and firewall limits.
   Use existing warning and localized refusal patterns, in eight languages.
7. **Trust boundary:** binding RCON to a Docker address does not stop an
   operator from routing VPN traffic to that address. Correct that claim
   in SPEC/ADR. The sidecar and its namespace peers are trusted; the game
   inherits its reachable networks and loopback services. Do not add a new
   scanner for namespace peers or a VPN-specific firewall manager.
8. **Trusted headers:** learn all Docker ranges from the sidecar, even
   when Foreman runs on the host, before reporting ranges known. Preserve
   fail-closed behavior when inspect fails, and retain previously learned
   ranges. Docker does not reveal VPN interface ranges. Documentation must
   prohibit trusting the sidecar's VPN address or a broad VPN CIDR; use
   only the actual separate proxy's individual address, or local sign-in.
   Do not hard-code Tailscale address ranges in the product.
9. **Tailscale reference:** use kernel networking (`TS_USERSPACE=false`),
   a persistent state volume, `/dev/net/tun`, and the required sidecar
   capability. Do not give the game additional capabilities. Pin the tested
   image tag/digest when preparing the runnable example. Userspace mode is
   an optional exploratory check, not a second promised configuration.
10. **Reboot:** test game recovery after Docker/host restart on a disposable
    host. If it fails, bring the observed failure to Todd before declaring
    acceptance; do not silently waive vision principle 2 or add auto-start.

## Architecture

Keep the implementation inside the existing config, Docker, game and web
boundaries. A private resolved-network value may carry Docker mode, sidecar
ID, RCON address and publication status through preparation. No general VPN
provider interface or stored sidecar model is needed. This private code is
easy to refactor later; persist no new world setting.

Extend container inspect only with fields needed for this work:
`HostConfig.NetworkMode`, `NetworkSettings.Ports`, and the game's environment
for its RCON bind address. Sidecar environment may contain credentials:
never log, render or include an inspect/environment dump in an error.
Persist the bind address in the created game configuration so monitor
reattachment can reconnect to the namespace that game actually joined,
even if the configured sidecar name now resolves to a replacement.
Validate the parsed address; never use arbitrary environment content as
a shell command or uncontrolled dial destination.

Reuse `gameNetwork`, `checkSharedNetwork`, `NetworkError`, `appendSubnets`,
`addSubnets`, `rconAddress`, and the existing RCON client. Audit every start
precheck caller, including restart, switch, mod changes and version updates.
Do not let the current host-mode early return bypass sidecar validation.
Do not mark an empty network list from the game inspect as complete evidence
in sidecar mode. Foreman restart must restore trust-range knowledge before
trusted headers can be accepted.

Use the low-disk warning's presentation pattern in `serverpage.go` and
`templates/server.html`, not a new settings page. Define the warning's
status field and new refusal kinds in T2 before dependent work starts.
Published-port status is evidence from inspection, not a connectivity probe.

## Tasks and ownership

All rows close no issues. Task PRs target the epic branch. Agents do not
edit STATUS until the fold; their PR bodies include proposed STATUS text.
Sonnet means `claude-sonnet-5-5`, Opus means `claude-opus-5-5`.

| Task | Owner and model | Files owned | Done and dependency |
| --- | --- | --- | --- |
| T0: settle contract | Orchestrator; current model | `docs/SPEC.md`, `docs/adr/0006-vpn-sidecar-network.md`, this plan | Record the approved dedicated-sidecar restriction; correct security claims; retain manual recovery. After plan approval. |
| T1: config and Docker support | Builder; Sonnet high | `internal/config/`, `internal/docker/` including `dockertest`, `internal/gameenv/` | Read sidecar config, require the game network, expose inspect fields, model shared namespace and refusal cases accurately in fake Docker. After T0. Keep the startup refusal until T2 wires the feature, so the intermediate commit cannot enable incomplete support. |
| T2: game integration and trust | Builder; Opus high | `internal/game/` production code and unit tests; `cmd/foreman/main.go`, the final enablement in `internal/config/config.go` after T1; `internal/web/game_security_test.go` | Validate sidecar, join verified ID, bind/dial RCON on all paths, retain protected sign-in ranges, expose LAN warning status. All lifecycle/precheck paths covered. After T1. |
| T3: browser test fixture | Builder; Sonnet high | `internal/fakegame/`, `cmd/fakegame/` | Seed a sidecar and published-port variants; simulate empty game networks and bind-aware RCON. After T1 and T2's agreed environment contract; may run beside T2. |
| T4: UI and browser coverage | Builder; Sonnet high | `internal/web/gametext.go`, `internal/web/serverpage.go`, their tests, `internal/web/templates/server.html`, `internal/i18n/locales/`, `e2e/tests/foreman.ts`, new `e2e/tests/sidecar.spec.ts` | Localized refusal and nonblocking LAN warning; start/console/refusal tests, live updates, no-script and 320px accessibility coverage. After T2 and T3. |
| T5: real game and Tailscale recipe | Tester; Opus high | New `internal/game/*sidecar*_game_test.go`; `docs/OPERATOR.md`, `README.md`; an example under `docs/examples/` if useful | Real-container coverage across the three supported game lines; executable Tailscale setup and manual acceptance checklist. After T2; may run beside T4. Reuse existing harness, coordinate any shared-helper edit. |
| T6: acceptance and fold | Orchestrator plus builder; Opus low for fold | `docs/STATUS.md`, `docs/SPEC.md`, ADR 0006, this plan | Complete checks and real Tailscale evidence; fold lasting decisions and measured gaps into docs, remove this plan, prepare epic PR into main. After all tasks, reviews and accepted results. |

Waves: T0, then T1, then T2/T3, then T4/T5, then T6. T2 and T3 must
agree the environment contract before concurrent work. T4 owns e2e fixture
changes; T3 owns fakegame changes. T5's new game-test file is excluded from
T2 ownership. The orchestrator runs full suites under the repository load
rules; agents run only the scoped checks required by epic-run.

## Verification and acceptance

Automated checks must cover these invariants, not merely mirror helpers:

- Ordinary mode still starts, reconnects and publishes its game port.
- Missing/stopped/host-network/nested-namespace/off-network sidecars fail
  before destructive world work. Inspection failures fail closed. Cover
  the dedicated-sidecar restriction and trusted-proxy overlap
  for IPv4 and IPv6 ranges visible to Docker.
- Replacing the named container between validation and creation cannot
  attach the game to the unchecked replacement.
- Shared-network games have no port/expose declarations. The real Docker
  harness proves Docker accepts the generated spec.
- Initial RCON, a dropped connection, Foreman restart and monitor reattach
  work. A stale game's RCON must not retarget a newly named sidecar.
- Sidecar ranges protect trusted-header sign-in with Foreman both inside
  and outside Docker, before first start, across monitor refresh, and
  after restart. A failed inspect cannot produce known-but-empty ranges.
- Missing publication warns without blocking start; publication clears
  the warning after a fresh start. Errors and warnings expose no secrets.
- The real game reaches in-game and answers RCON on pinned 1.1, 2.0 and 2.1
  images. Reuse a locally available image as a simple namespace holder;
  this proves Docker integration without requiring VPN credentials in CI.
  Probe a second namespace interface: RCON answers on the private game
  address and not on the other interface or loopback.
- Save/stop, restart and world switching work through RCON; failures leave
  worlds recoverable. Restart recovery with unavailable RCON must follow
  existing explicit force-stop behavior, never claim a save succeeded.

Live Tailscale acceptance uses a separate test stack and disposable world,
not Todd's active game. Obtain any needed credential through the secret
tool at execution time. No authentication values belong in the repository,
plan, PR, command output or test evidence. Record image versions/digests,
Docker version, test topology and each result:

1. Start through Foreman; console and Players work.
2. A LAN client joins the published host address while a client on another
   network joins the sidecar's Tailscale address. Both play concurrently.
3. RCON works from Foreman but connections to the LAN host and Tailscale
   address on TCP 27015 fail. Do not advertise the Docker game subnet or
   forward arbitrary Tailscale traffic to the private RCON address.
4. Remove publication by recreating the test sidecar, then restart the
   game: warning appears, remote VPN play works, LAN publication is absent.
5. Restart and separately recreate the sidecar while the test game runs.
   Record actual symptoms, restart the game through Foreman, and confirm
   both clients can rejoin. Do not expect a new sidecar-monitoring warning.
6. Restart Foreman only: the game stays running and RCON reattaches.
   Gracefully save/stop, restart, switch worlds and switch back.
7. Test the trusted-header boundary and constrain the sidecar's VPN access
   to required peers/services. No Docker API or proxy service is exposed
   through the sidecar. Document that a VPN ACL is operator configuration.
8. On a disposable host, restart Docker or reboot with game and sidecar
   running; verify recovery order and eventual game/RCON readiness.
   Do not restart Todd's host as part of ordinary automated tests.

Code review runs for each implementation task; security review covers T1,
T2 and the Tailscale recipe; accessibility review covers T4. End with the
integration review, `make verify`, `make e2e`, `make test-game` and
`make e2e-game` as required by WORKFLOW. Live Tailscale and reboot results
are explicit acceptance evidence, not implied by those commands. Report
any unavailable check as outstanding. Todd merges the final epic PR.

## Sources and progress

- [Tailscale Docker parameters](https://tailscale.com/docs/features/containers/docker/docker-params): kernel mode, persisted state, routing and proxy configuration.
- [Tailscale userspace networking](https://tailscale.com/docs/concepts/userspace-networking): separate mode, not assumed equivalent to the kernel reference setup.
- Local: ADR 0006, SPEC 4.13 and 14.16; architecture pass read the relevant
  config, container creation, launch, monitor and subnet code.

2026-11-14: architecture pass completed; plan drafted. Todd chose a dedicated
sidecar on the game Docker network only. Overall plan approval remains
pending. No implementation or live Tailscale test has run. Keep this
progress record on the epic branch;
the fold moves lasting evidence into STATUS and removes the plan.
