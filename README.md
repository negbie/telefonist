# Telefonist

A web-based management interface, providing a WebSocket API and web UI for controlling baresip SIP user agents.

## About

Telefonist is a standalone application that uses a web interface for managing SIP calls. It provides:

- WebSocket server for real-time communication
- Web UI for sending commands and viewing events
- Real-time event and response streaming

## Prerequisites

To build Telefonist, you need the following tools:

- **Go 1.26+**
- **C/C++ Compiler** (e.g., `gcc`, `g++`)
- **Make** and **CMake**
- **Git** and **Wget**
- **Zig** (only required for `make static` to produce fully static binaries)
- **ALSA development headers** (e.g., `libasound2-dev` on Debian/Ubuntu, only for `make alsa`)
- **UPX** (optional, for `make compress`)

## Build

Telefonist uses a `Makefile` to manage the complex build process of its dependencies (OpenSSL, Opus, baresip, etc.).

### Standard Build
```bash
make
```
This builds a dynamically linked version of `telefonist`.

### ALSA Support
```bash
make alsa
```
Builds with ALSA audio support. Note: requires `libasound2-dev`.

### Fully Static Build
```bash
make static
```
Produces a fully static binary using the **Zig** toolchain and **musl**. This binary has zero runtime dependencies and can run on any Linux system.

## Usage

Run telefonist with its various configuration flags.

### CLI Flags

- `-data_dir` - Directory for configuration files and runtime data (default: "data")
- `-ui_address` - UI listen address (default: "0.0.0.0:8080")
- `-ui_admin_password` - UI admin password (default: "telefonist")
- `-sip_address` - SIP listen address like 0.0.0.0:5060 (default: "")
- `-ctrl_address` - Local control listen address (default: "127.0.0.1:4444")
- `-max_calls` - Maximum number of incoming calls (default: 10)
- `-use_alsa` - Use ALSA for audio (uncomments alsa lines in config)
- `-rtp_interface` - RTP interface like eth0 (default: "")
- `-rtp_ports` - RTP port range (default: "10000-11000")
- `-rtp_timeout` - Seconds after which a call with no incoming RTP packets will be terminated (default: 10)
- `-tls_cert` - Path to TLS certificate file
- `-tls_key` - Path to TLS key file
- `-version` - Print version and exit

## Web Interface

Once started, open your browser to `http://localhost:8080` (or whatever ui_address you configured). You'll see:

- A login prompt. Use telefonist as username and your configured ui_admin_password (default: telefonist)
- A command input field to send commands to baresip
- A search field to filter displayed events, SIP messages and logs
- Real-time display of events and responses, SIP messages, audio events, etc.
- A testfile editor to write and execute automated test sequences
- A testfile compare browser to view past test runs (events and wav's) to compare them

## Architecture

Telefonist is a thin application layer on top of baresip:

1. Creates a baresip instance
2. Sets up a WebSocket hub that bridges telefonist channels to WebSocket clients
3. Provides an HTTP server with a web UI

## Writing and Running Tests

Telefonist allows you to write and execute automated test sequences (testfiles) directly from the web UI.

### Testfile Syntax

Testfiles are line-based and support the following syntax:

- **Comments**: Lines starting with `#` are ignored.
- **Commands**: Each line defines a test case: `Name: command1 | duration | command2`
  - Example: `CallTest: dial sip:user@host | 5s | hangup`
- **Chaining**: Use `|` to separate multiple commands or to insert delays.
- **Durations**: Use `10s`, `500ms`, `2m`, etc., to wait between steps.
- **Wav Shortcuts**:
  - `;input_wav=file.wav` - Automatically configures `audio_source` to use `file.wav`.
- **Metadata**:
  - `_run <count>` - Repeat the entire testfile `<count>` times.
  - `_ignore <event1>,<event2>` - List of events to ignore during analysis.
  - `_define <VAR> <value>` - Define a macro that will be replaced in subsequent lines.
  - `_hash <expected_hash>` - Verify the final state against a specific hash.
  - `_webhook <url>` - Send the final test result to this webhook URL (e.g., MS Teams).

  Refer to the [baresip documentation](https://github.com/baresip/baresip/wiki) for command details.

### Quick Start Example

1. **Start Telefonist**: 
   ```bash
   ./telefonist -sip_address "0.0.0.0:5060"
   ```

2. **Create a Test**: Open the UI at `http://localhost:8080`, define a new project, and create a testfile with the following content (replace IPs with your local addresses):

   ```bash
   _hash 3ce5536071322de9
   _define ua1 sip:alice@192.168.1.100
   _define ua2 sip:bob@192.168.1.100
   _define ua3 sip:charlie@192.168.1.100
   _ignore TRANSFER, CALL_CLOSED
   _run 1

   uanew <ua1;transport=udp>;regint=0;input_wav=alice.wav
   uanew <ua2;transport=udp>;regint=0;input_wav=bob.wav
   uanew <ua3;transport=udp>;regint=0;input_wav=charlie.wav

   # Attended Transfer 
   uafind ua1
   dial ua2|2s|accept|6s
   atransferstart ua3|2s|accept|6s
   uafind ua2|atransferexec|2s|accept|6s|hangup
   uadelall
   ```

3. **Understanding Directives**:
   - `_hash`: A unique identifier (checksum) representing the expected sequence of events. If the run matches this hash, the test passes.
     > [!TIP]
     > For your first run, leave `_hash` empty. The final test result will reveal the actual hash, which you can then copy into your testfile for future validation.
   - `_ignore`: A comma-separated list of events to exclude from the hash calculation (e.g., `TRANSFER`, `CALL_CLOSED`).
   - `_define`: Creates reusable macros for SIP URIs or other configuration strings.
   - `_run`: Specifies how many times to repeat the entire sequence.

4. **Execution**: Click **Run**. Once finished, the UI will display the PASS/FAIL status along with the generated hash.

## License

This project is licensed under the **GNU Affero General Public License Version 3 (AGPL-3.0)**. See the [LICENSE](LICENSE) file for the full text.
