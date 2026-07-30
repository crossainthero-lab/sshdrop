package diagnostics

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func Translate(err error) string {
	if err == nil {
		return "ok"
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return "DNS lookup failed. Check that the hostname is spelled correctly and that your network DNS can resolve it."
	}
	var op *net.OpError
	if errors.As(err, &op) {
		if strings.Contains(strings.ToLower(op.Error()), "timeout") {
			return "Connection timed out. Check the host address, port, firewall, VPN, and whether SSH is running on the remote device."
		}
		return "Network connection failed. Check that the device is reachable and that the SSH port is open."
	}
	var keyErr *knownhosts.KeyError
	if errors.As(err, &keyErr) {
		if len(keyErr.Want) == 0 {
			return "The host key is unknown. Confirm the fingerprint before trusting this device."
		}
		return "The host key has changed. This can mean the remote device was rebuilt, but it can also indicate a security problem. Verify the fingerprint before updating known_hosts."
	}
	if errors.Is(err, os.ErrPermission) {
		return "Permission denied. Check local file permissions or remote directory permissions."
	}
	if strings.Contains(err.Error(), "unable to authenticate") || strings.Contains(err.Error(), "permission denied") {
		return "SSH authentication failed. Check your username, SSH agent, identity file, passphrase, and server-side authorized_keys."
	}
	if strings.Contains(err.Error(), "too open") || strings.Contains(err.Error(), "bad permissions") {
		return "SSH key permissions are too open. Restrict the private key file, for example with chmod 600 on Linux or macOS."
	}
	var exit *ssh.ExitError
	if errors.As(err, &exit) {
		return fmt.Sprintf("The remote SSH operation failed: %s", exit.Msg())
	}
	if strings.Contains(strings.ToLower(err.Error()), "no space left") {
		return "The destination disk is full. Free space on the destination and retry."
	}
	return err.Error()
}
