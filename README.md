# Telefonist

A web-based management interface, providing a WebSocket API and web UI for controlling baresip SIP user agents.

<img width="800" height="600" alt="Image" src="https://github.com/user-attachments/assets/0603eb15-e2ed-4957-8ac1-0a9dc9656ef4" />

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
- `-sip_listen` - SIP listen address and base port for agents (default: "0.0.0.0:5060")
- `-ctrl_address` - Local control listen address (default: "127.0.0.1:4444")
- `-max_calls` - Maximum number of incoming calls for agents (default: 10)
- `-use_alsa` - Use ALSA for audio in agents (uncomments alsa lines in agent config)
- `-rtp_interface` - RTP interface for agents like eth0 (default: "")
- `-rtp_ports` - RTP port range for agents (default: "10000-11000")
- `-rtp_timeout` - Seconds after which a call with no incoming RTP packets will be terminated in agents (default: 10)
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

Telefonist follows a multi-process **Agents Architecture**:

1. **Master Hub**: A pure Go orchestrator that manages projects, testfiles, and provides the Web/WebSocket interface.
2. **Agents**: Isolated and disposable `baresip` instances spawned as separate processes for each test run or persistent user agent.
3. **Control Interface**: The Master Hub controls agents via localized TCP control interfaces, ensuring stability and crash isolation.

## Writing and Running Tests

Telefonist allows you to write and execute automated test sequences (testfiles) directly from the web UI.

### Testfile Syntax

Testfiles are line-based and support the following syntax:

- **Comments**: Lines starting with `#` are ignored.
- **Commands**: Each line defines a test case: `command1 | duration | command2`
  - Example: `dial sip:user@host | 5s | hangup`
- **Chaining**: Use `|` to separate multiple commands or to insert delays.
- **Durations**: Use `10s`, `500ms`, `2m`, etc., to wait between steps.
- **Agent Targeting**: Prefix a command with an agent alias followed by a colon to target a specific agent.
  - Example: `ua1:dial ua2` sends the `dial ua2` command to the agent with alias `ua1`.
  - Default: Commands without a prefix are sent to the last created or "active" agent.
- **Wav Shortcuts**:
  - `;input_wav=file.wav` - Automatically configures `audio_source` to use `file.wav`.
- **Metadata**:
  - `_hash <expected_hash>` - Verify the final state against a specific hash.
  - `_ignore <event1>,<event2>` - List of events to ignore for the hash calculation.
  - `_accept <event1>,<event2>` - List of events to accept for the hash calculation.
  - `_define <VAR> <value>` - Define a macro that will be replaced in subsequent lines.
  - `_webhook <url>` - Send the final test result to this webhook URL (e.g., MS Teams).
  - `_run <count>` - Repeat the entire testfile `<count>` times.

  Refer to the [baresip documentation](https://github.com/baresip/baresip/wiki) for command details.

### Quick Start Example


1. **Start Telefonist**: 
   ```bash
   ./telefonist
   ```

2. **Create a Test**: Open the UI at `http://localhost:8080`, define a new project, and create a testfile with the following content (replace the SIP addresses with your own):

   ```bash
   _hash Actual_hash
   _define ua1 sip:alice@192.168.1.100
   _define ua2 sip:bob@192.168.1.100
   _define ua3 sip:charlie@192.168.1.100
   _accept CALL_RINGING, CALL_ESTABLISHED, CALL_RTPESTAB, CALL_CLOSED, AUDIO_REPORT
   _run 1

   uanew <ua1;transport=udp>;auth_pass=secret1;input_wav=alice.wav
   uanew <ua2;transport=udp>;auth_pass=secret2;input_wav=bob.wav
   uanew <ua3;transport=udp>;auth_pass=secret3;input_wav=charlie.wav

   # Attended Transfer 
   ua1:dial ua2|2s|ua2:accept|6s
   ua2:atransferstart ua3|2s|ua3:accept|6s
   ua2:atransferexec|6s|ua1:hangup
   uadelall
   ```

3. **Understanding Directives**:
   - `_hash`: A unique identifier (checksum) representing the expected sequence of events. If the run matches this hash, the test passes.
     > [!TIP]
     > For your first run, leave `_hash` empty. The final test result will reveal the actual hash, which you can then copy into your testfile for future validation.
   - `_accept`: A comma-separated list of events to accept for the hash calculation (e.g., `CALL_RINGING`, `CALL_ESTABLISHED`).
   - `_define`: Creates reusable macros for SIP URIs or other configuration strings.
   - `_run`: Specifies how many times to repeat the entire sequence.

4. **Execution**: Click **Run**. Once finished, the UI will display the PASS/FAIL status along with the generated hash.

## License

This project is licensed under the **GNU Affero General Public License Version 3 (AGPL-3.0)**. See the [LICENSE](LICENSE) file for the full text.
