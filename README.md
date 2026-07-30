# SSHDrop

SSHDrop is a fast terminal file-transfer app for moving files between your local
machine and SSH devices over SFTP. Run `sshdrop`, pick a saved device, browse
local files on the left and remote files on the right, then upload or download
with a keyboard shortcut.

It is written in Go and builds to standalone binaries for Linux and macOS.

## Features

- Dual-pane terminal file manager built with Bubble Tea and Lip Gloss.
- SFTP transfers over SSH using normal SSH keys, SSH agent, passphrase-protected keys, and interactive password authentication.
- Device profiles stored in `~/.config/sshdrop/config.yaml` on Linux and the macOS user config location on macOS.
- Imports host aliases from `~/.ssh/config`.
- Secure known-host verification with explicit confirmation for unknown hosts.
- Upload and download files, multiple selections, and recursive directory trees.
- Transfer queue with progress, bytes transferred, speed, completion state, errors, retry-friendly queue behavior, and cancellation.
- Direct commands for uploads and downloads without opening the full TUI.
- Partial-file writes followed by atomic rename so failed transfers do not replace good destination files.

## Installation

Build from source:

```bash
git clone https://github.com/crossainthero-lab/sshdrop.git
cd sshdrop
go build -o sshdrop ./cmd/sshdrop
sudo install sshdrop /usr/local/bin/sshdrop
```

Manual Linux install after downloading a release archive:

```bash
tar -xzf sshdrop_0.1.0_linux_amd64.tar.gz
sudo install sshdrop /usr/local/bin/sshdrop
```

Manual macOS Apple Silicon install:

```bash
tar -xzf sshdrop_0.1.0_darwin_arm64.tar.gz
sudo install sshdrop /usr/local/bin/sshdrop
```

Safe install script, after release assets exist:

```bash
curl -fsSL https://raw.githubusercontent.com/crossainthero-lab/sshdrop/main/scripts/install.sh | sh
```

The script detects Linux or macOS, AMD64 or ARM64, downloads the matching
release archive, verifies `checksums.txt`, and explains when elevated
permissions are needed.

## Quick Start

```bash
sshdrop device add
sshdrop
```

Select a device, press `Enter`, browse both panes, press `Space` to select one
or more items, then press `u` to upload from local to remote or `d` to download
from remote to local.

## Adding A Device

```bash
sshdrop device add
```

The wizard asks for a friendly name, host, username, port, SSH key or agent use,
tests the connection, then saves the profile. Passwords and passphrases are only
used for the current connection attempt and are never saved.

Example device names:

```text
MacBook
Linux Server
Raspberry Pi
Home Desktop
Nintendo Switch
```

## Keyboard Controls

```text
↑ / ↓        Move selection
Enter        Open directory
Backspace    Parent directory
Tab          Switch between local and remote panes
Space        Select or deselect item
u            Upload selected local files
d            Download selected remote files
n            Create directory
r            Rename file or directory
x            Delete file or directory
c            Cancel active transfer
h            Show keyboard help
q            Quit
```

## Direct Commands

```bash
sshdrop connect macbook
sshdrop upload ./video.mp4 macbook:/home/william/Videos/
sshdrop download server:/var/log/app.log ./
sshdrop devices
sshdrop device test macbook
sshdrop device remove macbook
sshdrop doctor
sshdrop version
```

SSH-style targets are supported:

```text
device:/remote/path
user@hostname:/remote/path
```

## SSH Authentication

SSHDrop tries the SSH agent first when `SSH_AUTH_SOCK` is available. It then
tries the configured identity file, or common default keys such as
`~/.ssh/id_ed25519`, `~/.ssh/id_ecdsa`, and `~/.ssh/id_rsa`.

Passphrase-protected keys are supported through an interactive prompt. Password
authentication is supported when the server allows it. Plaintext passwords are
never written to disk.

## SSH Config Support

Host aliases from `~/.ssh/config` are recognized automatically and shown beside
saved SSHDrop profiles. Saved profiles take precedence over imported aliases.

## Troubleshooting

Run:

```bash
sshdrop doctor
sshdrop device test <device>
sshdrop --verbose device test <device>
```

`device test` explains common DNS, network, authentication, key-permission,
permission, and host-verification failures in user-facing language.

## Security Model

- Uses SFTP APIs over SSH for file operations.
- Uses `known_hosts`; unknown hosts require explicit fingerprint confirmation.
- Does not disable host-key verification.
- Does not execute arbitrary remote shell commands for normal file operations.
- Does not log passwords, passphrases, private keys, or sensitive environment variables.
- Writes config files with restrictive permissions.
- Requires confirmation for deletion and transfer overwrites.
- Writes to partial temporary files and renames them only after successful transfer.

## Building From Source

```bash
go fmt ./...
go vet ./...
go test ./...
go build ./cmd/sshdrop
```

Cross-compile examples:

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o dist/sshdrop-linux-amd64 ./cmd/sshdrop
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o dist/sshdrop-linux-arm64 ./cmd/sshdrop
GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -o dist/sshdrop-darwin-amd64 ./cmd/sshdrop
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -o dist/sshdrop-darwin-arm64 ./cmd/sshdrop
```

## Supported Platforms

- Linux x86_64
- Linux ARM64
- macOS Intel
- macOS Apple Silicon

## Roadmap

- Richer in-TUI device creation instead of the current CLI wizard entry point.
- Optional isolated SSH/SFTP integration test harness for CI.
- Transfer history and resumable retry metadata.
- Homebrew packaging after the first release.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Security-sensitive changes must preserve
host-key verification and must not persist secrets.
