# Hackathon product demo specification

Status: implementation in progress, 2026-11-10. This extends the existing product architecture in `personal-apps-v1.md`, `design.md`, and `v1-plugin-technical.md`; it does not introduce another runtime or signing protocol.

## Product contract

Lintel's primary workflow is: the user describes an app to a locally configured coding agent; the agent generates a normal plugin package; Lintel validates it, previews its native pages without signing, and installs only after separate native permission review. The agent is not an OS-sandboxed process. Installed apps run in the existing moejs sandbox.

The Security-track demo makes the approval boundary visible with a fixed NEAR example. The example is an ordinary, explicitly installed folder package, not a seeded official app and not a special host backdoor. Real model generation and the fixed example are separate evidence; neither may be presented as the other.

## NEAR example package

Path: `internal/plugin/testdata/examples/near-approval/` with `plugin.json` and `index.js`.

- ID `com.example.nearapproval`, version `1.0.0`, main `index.js`. When the same source is proposed through ACP, its manifest ID must first be changed to the required `local.personal.*` namespace; the fixed folder-install example itself keeps its example ID.
- Permissions: `sign: true`, chains exactly `near:testnet`; no network, storage, balances, or settings.
- Navigation declares `overview`, `transfer`, and `limited` pages. Overview only explains the security boundary and links to the other pages; it never signs.
- `transfer` sends exactly 0.001 NEAR (`1000000000000000000000` yoctoNEAR) to the configured testnet receiver using the existing synchronous `host.requestNearSign`. The initial receiver is `bob.testnet` and is documented as a replaceable source constant for a reviewer-controlled recipient. No secrets or arbitrary transaction fields are accepted.
- `limited` demonstrates a request to `guest-book.testnet` using the existing signing path. With a restricted function-call key, an unauthorized method must be refused before approval; with a full-access key the user still sees the native approval and may reject. The example does not claim automatic malicious-request detection.
- Signing errors are rendered as a visible `Request not signed` page with the error text. Success renders the returned transaction hash. No hash is fabricated, no broadcast status is inferred, and no secret is logged or stored.
- The package is available through Apps → Install from folder and the existing package validation APIs. It is not embedded into the official first-launch seed, preserving the two read-only official examples.

## Demonstration state and evidence

1. A real ACP generation is validated separately from the fixed package. The prepared candidate remains pending in the same process and must be freshly prepared within its 30-minute lifetime.
2. Preview executes declared candidate pages with both signing callbacks refused, leaves status pending, and does not call native install confirmation.
3. Native installation approval is the only installation path. Rejecting it leaves no installed app.
4. The example's overview has no signing side effect. The transfer page calls the signer exactly once. A user rejection is visible and no hash is shown. A successful signer result displays exactly the returned hash.
5. Live testnet approval requires a separately funded testnet account and reviewer-controlled recipient. Actual transaction/explorer evidence is recorded only after it is observed; mocked tests never count as live chain verification.
6. The video uses a freshly prepared candidate rather than waiting for a model, and labels it as prepared. The README leads with agent-authored apps, then the security demonstration.

## Test contract

- Manifest validation rejects unexpected permissions and undeclared pages; all declared pages compile.
- Overview makes zero signing calls and provides both declared navigation links.
- Transfer passes exact chain, receiver, description, action type and amount; a hash is rendered verbatim; a rejection is visible without a success hash.
- Preview of the same source refuses signing and preserves pending state.
- Folder installation remains unsigned and does not change official example integrity or first-launch seeding.
- The agent prompt asks for a normal native-page app; actual generation, preview, install, and update results are recorded separately from unit tests.
- Full build, tests, vet, race checks for touched concurrency boundaries, and native screenshot checks are run before publication.

## ACP compatibility found during live acceptance

Claude Agent ACP 0.102.0 advertises 868 unique model choices in `session/new.configOptions`. The previous per-selector limit of 128 rejected the entire session. ACP wire parsing retains a 4 MiB message bound and accepts at most 1024 options per selector; the native select renders at most 128. Parsing still rejects duplicate values, missing names/IDs, oversized strings, mixed group shapes, excessive groups/options and invalid value types. Non-select types remain non-editable. A session's current value is not added as a fake selectable value when it falls outside the rendered list. This compatibility change grants no new agent, installation or wallet permission.

Out of scope: wet Intents, autonomous payments or budgets, new signing actions, arbitrary form input, automatic threat scoring, OS sandboxing of the coding agent, and automatic installation or approval.
