# Telefonist Architecture

Telefonist provides a web interface for automated SIP testing, call recording, and real-time monitoring based on baresip.

## Core Principles

- **Single Binary**: Fully static builds (via Zig/musl) containing all dependencies.
- **Centralized Data**: All persistent state (DB, recordings, sounds) is managed under a configurable `--data_dir`.
- **Event-Driven**: Asynchronous communication between the SIP core and web clients via WebSockets.
- **Hash-Based Verification**: Stable, normalized event streams used for automated test pass/fail validation.

---

## High-Level Runtime Flow

1. **Initialization**
   - Parse CLI flags (`pkg/telefonist/flags.go`).
   - Initialize `DataDir` and generate/patch `baresip` configuration (`pkg/telefonist/config.go`).
   - Extract embedded assets (sounds) and initialize the SQLite `TestStore`.
   - Create a `telefonist.Baresip` instance (CGO wrapper).

2. **Orchestration (WsHub)**
   - Start the `WsHub` Select-loop:
     - Connects `telefonist` events/responses to all WebSocket clients.
     - Dispatches incoming commands to the SIP core or internal test handlers.
     - Manages hierarchical recording persistence and cleanup.

3. **Command Processing**
   - **Command Chain**: Pipelined execution (`cmd|delay|cmd`) handled in `pkg/telefonist/cmdchain.go`.
   - **Expansion**: Automatic shortcut rewriting (like `;input_wav=file.wav`).
   - **Test Orchestration**: `ws_testfile_inline.go` manages the lifecycle of automated test runs.

4. **Persistence**
   - **TestStore**: SQLite backend for projects, testfiles, and runs.
   - **Hierarchical Storage**: Recorded WAVs are organized as `recorded_wavs/<project>/<testfile>/<runID>/`.

---

## Project Structure & Responsibilities

### Layout
- `cmd/telefonist/`: Main application entry point (`main.go`).
- `pkg/telefonist/`: Core application logic, HTTP/WS handlers, and storage.
- `pkg/gobaresip/`: Go wrapper around the `baresip` C library.
- `assets/`: Embedded zip files for UI and audio files.
- `configs/`: Embedded configuration templates and patching logic.

### Key Modules

#### [Entry & Running]
- `run.go`: Wires together the hub, SIP core, and HTTP server.
- `flags.go`: Defines the CLI interface (`--data_dir`, `--use_alsa`, etc.).

#### [Storage & State]
- `teststore.go`: Manages the SQLite database and hierarchical filesystem operations (WAV directories).
- `config.go`: Handles dynamic configuration generation and sound extraction.

#### [Communication]
- `ws_hub.go`: The central dispatcher for all system messages.
- `websocket.go`: Low-level WS transport and minimal HTTP server.
- `ws_testfile_inline.go`: High-level orchestration for UI-driven test execution.

#### [Logic & Automation]
- `cmdchain.go`: Command parsing, pipelined delays, and path expansion.
- `training.go`: Normalization and hashing logic for test verification.

---

## Data Management

### Persistent Storage
All data is stored relative to the `--data_dir` flag (default: `data`):
- `telefonist_tests.db`: SQLite database.
- `config`: Generated baresip configuration.
- `sounds/`: Extracted SIP audio files.
- `recorded_temp/`: Stored wav recordings.
- `recorded_wavs/`: Temporary wav recordings.

---

## Concurrency Model

1. **Hub Execution**: Runs in a single dedicated goroutine to prevent state race conditions.
2. **WebSocket Pumps**: Each client has two goroutines (`readPump`/`writePump`) for non-blocking I/O.
3. **Webhooks**: Notification are non-blocking or batched in separate goroutines.

---

## Build System

The project uses a sophisticated Makefile for cross-platform portability and optimized deployments:
- `make static`: Leverages `zig cc` and `musl` for a 100% dependency-free binary.
- `make compress`: Applies `upx --lzma` to the static binary for minimal footprint.
- `make alsa`: Dynamically patches the library and binary for ALSA support on Linux.

---

## Navigation for Developers

1. Start at `cmd/telefonist/main.go` → `telefonist.Run()`.
2. Explore `pkg/telefonist/ws_hub.go` for the core message dispatching logic.
3. Review `pkg/telefonist/teststore.go` for data persistence.
4. See `pkg/telefonist/config.go` for how the SIP core is configured.