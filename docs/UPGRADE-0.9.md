# Telefonist 0.9 implementation and upgrade notes

This change addresses the September 30 review with a durable runner, explicit failures, safer artifact storage, bounded processing, and a run-focused workspace. The historical review describes the previous implementation; its original line numbers no longer apply.

## Upgrade

Stop Telefonist and back up the complete data directory, including SQLite and recordings, before replacing the binary. Schema additions happen automatically. Interrupted RUNNING rows become ERROR when the database is reopened. Existing recording paths remain readable; renaming tests/projects now changes metadata only. New artifacts use `recorded_wavs/runs/<run-id>/...`.

The UI now binds to `127.0.0.1:8080`. Configure `-ui_address` explicitly for remote access. The username is **admin**. When omitted, the password and API key are generated in `<data_dir>/credentials.json` with private Linux permissions. Credentials are not printed to logs. Database passwords remain plaintext; GET responses do not expose them. Sessions are independent, expire server-side after 24 hours, and are revoked by logout, including existing WebSocket connections.

Existing hashes retain their original byte format and algorithm. Hashing consumes original agent events before enrichment; added timestamps, run IDs and sequence numbers never enter the hash. No baseline migration is required. `_hash` without a value records evidence but produces UNVERIFIED. `_include` and a populated `_hash` both apply when present. PASS requires a validator; a delay-only successful run is UNVERIFIED. `_run 0` produces SKIPPED.

Schedules now default to UTC. Add `CRON_TZ=Europe/Berlin` before an expression to preserve German local-time scheduling. Repeated triggers for a queued/running project/test are coalesced. Settings → Retention & delivery shows next schedule times, webhook retry counts, pending artifact deletions, and the retention setting.

Build from a fresh native source/staging directory when adopting these changes. Reusing an old patched source tree can retain removed patches. Git dependencies are pinned to commit IDs and archive downloads are SHA-256 checked. Version overrides need matching refs/checksums. Audio-player initialization errors are no longer converted to success. Nonstandard transfer behavior is disabled by default; `make TRANSFER_COMPAT=1` explicitly enables the historical no-hold/no-Replaces patches. Use a fresh source tree when switching that option off.

## What changed

| Review area | Implementation |
|---|---|
| Execution and shutdown | Dedicated runner, bounded manual queue, asynchronous bounded saved-file loading, acknowledged commands, cancellable actor calls/drains, joined subprocess cleanup, run and evaluator deadlines. |
| Evidence and verdicts | Run created before execution; executed source, validator, failure reason and finish time stored; ABORTED/ERROR/SKIPPED/UNVERIFIED preserved; RTCP and display-filtered logs included; timestamps, elapsed milliseconds and event sequence recorded. |
| Telemetry integrity | Master ingestion backpressures; native callback overflow is reported; final NATIVE_DONE flush handshake is required; missing completion or overflow invalidates the run. UI delivery is best effort and cannot establish a verdict. |
| Storage | Run-ID artifact paths, containment/symlink checks, transactional rename/delete, durable deletion queue, source preservation on failed recording transfer, per-test retention and pinned baselines. Save/version updates are transactional with optimistic revision checks. |
| Parser | Exact account argument/target resolution, deterministic noncascading macros, quoted/secret pipe handling, bounded repeats/delays, directive/command preflight errors. |
| Native audio | G.722 capacities checked before codec calls, bounded WAV reads, recording-name sequence, allocation cleanup, legacy audio-report format preserved. Player-error suppression removed. |
| Transport | Bounded strict netstrings, single-master proxy ownership, connection cancellation and write deadlines, opaque request IDs, negative acknowledgement propagation, IPv6-safe synthetic PCAP export. |
| Browser | Captured-file save identity, preserved in-flight edits, per-tab draft recovery, save conflicts, HTTP failure handling, reconnect/backoff/deduplication/gap notices, bounded queues and diff worker, stale run-fetch guards, escaped settings values. |
| Workspace | Full-height tests/results/settings, optional live inspector, connection/run summary, readable navigation labels, full test names, resizable navigation, source/assertion/failure details, baseline pinning, protocol-preserving comparisons. |
| Operations | Private generated credentials, independent revocable sessions, request limits, explicit HTTP server lifecycle, batch status/cancel/JUnit endpoints, persistent webhook retries, pinned native dependencies, fail-fast patcher and Linux CI. |

## CI API

Authenticate with `X-API-Key`:

1. `POST /api/project/run?name=PROJECT` → **202** with `batch_id`.
2. `GET /api/batch?id=BATCH_ID` → active state and stored run results.
3. `DELETE /api/batch?id=BATCH_ID` → cancel the active batch.
4. `GET /api/batch/junit?id=BATCH_ID` → JUnit XML after completion; **409** while active. ERROR/ABORTED become errors, FAIL becomes failure, UNVERIFIED/SKIPPED become skipped tests.

The browser also uses `/api/runner`, `/api/operations`, and `/api/testrun/pin`. Pinning protects a run from automatic retention, not an explicit user deletion.

## Limits and deliberate choices

- Linux remains the supported SIP runtime. The non-Linux/no-cgo adapter exists only to test portable orchestration; native operations return an error.
- Event timestamps measure master ingestion, not packet capture. Synthetic PCAP reconstructs UDP frames from SIP text, supports IPv4/IPv6, skips malformed endpoints, and is **not** a wire capture of TLS/TCP or RTP.
- Runs have a 30-minute batch deadline, scripts a 5-second evaluator deadline, evidence a 32 MiB cap, and control messages a 256 KiB cap. Exceeding evidence/transport limits must produce an error rather than PASS. Large browser diffs fall back to explicitly labeled removed/added output.
- The presence module retains its documented first-state-per-subscription behavior. It does not implement a complete multi-dialog XML state machine. Use raw SIP NOTIFY evidence for assertions needing namespaces, full/partial updates or multiple dialogs.
- Audio reports retain their existing format to preserve hash baselines. RTCP is available to validators. No MOS or waveform-quality claim is made from packet counts.
- Existing unreferenced legacy WAV files are not automatically destroyed. The deletion queue tracks deletions initiated by this version; it cannot safely infer ownership of already orphaned evidence.
- The existing SIP renderer remains available. Participant-lane redesign, cross-pane call correlation, and full DOM virtualization are not part of this implementation. The current changes bound live history/backlogs, move diff work off the UI thread, and expose complete stored evidence.
- Kernel port handoff is not available through the current baresip configuration interface. Allocation validates the configured range and startup/registration failures become durable errors; another process can still win a port between selection and bind.
- Source snapshots redact recognized `auth_pass` values. Prefer centralized accounts over embedding secrets in test bodies or validator scripts. Per-tab draft recovery uses browser session storage.

## Verification

Portable Go tests, Go race tests, frontend syntax checks, and browser contract tests were run on Windows. The tests exercise cancellation and telemetry loss through the real runner/hub/SQLite path; preserve RTCP; check stale-save rejection; test artifact rename/deletion/failure retention; test IPv6 export, actual command acknowledgements, bounded netstrings, session revocation, and patch drift.

Browser layout inspection used a local static preview with empty API fixtures. It verified navigation and layout only, not SIP execution.

On Linux, run:

```sh
make JOBS=2
make test-native-bounds
go test -race ./pkg/...
node --test tests/web/contracts.test.mjs
```

The native bounds target uses ASan/UBSan. CI also has portable and native Linux jobs. Neither Linux compilation, sanitizer execution nor live SIP/media calls were run on this Windows host. Before production use, exercise registration success/failure, cancellation during registration and a call, calls that omit `uadelall`, transfer behavior, repeated audio playback, and recording finalization against your test endpoints.
