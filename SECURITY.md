# Security

SSHDrop uses SFTP over SSH and relies on SSH host-key verification through the
user's `known_hosts` file. Unknown hosts require explicit trust before they are
added.

Do not report security issues in public issues. Send a private advisory through
GitHub Security Advisories for this repository.

SSHDrop never saves plaintext passwords, passphrases, private keys, or SSH agent
contents. Configuration files are written with restrictive permissions.
