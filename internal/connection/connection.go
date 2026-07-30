package connection

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/crossainthero-lab/sshdrop/internal/config"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

type Options struct {
	Timeout       time.Duration
	Verbose       bool
	Password      func(prompt string) (string, error)
	ConfirmHost   func(host, fingerprint string) bool
	KnownHosts    string
	AllowPassword bool
}

type Client struct {
	Device config.Device
	SSH    *ssh.Client
	SFTP   *sftp.Client
}

func Dial(d config.Device, opts Options) (*Client, error) {
	if err := config.ValidateDevice(d); err != nil {
		return nil, err
	}
	if opts.Timeout == 0 {
		opts.Timeout = 10 * time.Second
	}
	auth, err := authMethods(d, opts)
	if err != nil {
		return nil, err
	}
	hostKey, err := hostKeyCallback(d, opts)
	if err != nil {
		return nil, err
	}
	cc := &ssh.ClientConfig{
		User:            d.User,
		Auth:            auth,
		HostKeyCallback: hostKey,
		Timeout:         opts.Timeout,
	}
	addr := net.JoinHostPort(d.Host, fmt.Sprintf("%d", d.Port))
	sshClient, err := ssh.Dial("tcp", addr, cc)
	if err != nil {
		return nil, err
	}
	sftpClient, err := sftp.NewClient(sshClient)
	if err != nil {
		_ = sshClient.Close()
		return nil, err
	}
	return &Client{Device: d, SSH: sshClient, SFTP: sftpClient}, nil
}

func (c *Client) Close() error {
	var err error
	if c.SFTP != nil {
		err = c.SFTP.Close()
	}
	if c.SSH != nil {
		if sshErr := c.SSH.Close(); err == nil {
			err = sshErr
		}
	}
	return err
}

func authMethods(d config.Device, opts Options) ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod
	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		conn, err := net.Dial("unix", sock)
		if err == nil {
			methods = append(methods, ssh.PublicKeysCallback(agent.NewClient(conn).Signers))
		}
	}
	keyFiles := []string{}
	if d.IdentityFile != "" {
		keyFiles = append(keyFiles, expandHome(d.IdentityFile))
	} else {
		keyFiles = append(keyFiles, defaultKeys()...)
	}
	for _, keyFile := range keyFiles {
		signer, err := signerFromFile(keyFile, opts.Password)
		if err == nil {
			methods = append(methods, ssh.PublicKeys(signer))
		}
	}
	if opts.AllowPassword && opts.Password != nil {
		methods = append(methods, ssh.PasswordCallback(func() (string, error) {
			return opts.Password("SSH password")
		}))
		methods = append(methods, ssh.KeyboardInteractive(func(user, instruction string, questions []string, echos []bool) ([]string, error) {
			answers := make([]string, len(questions))
			for i, q := range questions {
				ans, err := opts.Password(q)
				if err != nil {
					return nil, err
				}
				answers[i] = ans
			}
			return answers, nil
		}))
	}
	if len(methods) == 0 {
		return nil, errors.New("no SSH authentication methods available; start ssh-agent, add an identity file, or allow password authentication")
	}
	return methods, nil
}

func signerFromFile(path string, password func(string) (string, error)) (ssh.Signer, error) {
	key, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	signer, err := ssh.ParsePrivateKey(key)
	if err == nil {
		return signer, nil
	}
	if _, ok := err.(*ssh.PassphraseMissingError); ok && password != nil {
		pass, passErr := password(fmt.Sprintf("Passphrase for %s", path))
		if passErr != nil {
			return nil, passErr
		}
		return ssh.ParsePrivateKeyWithPassphrase(key, []byte(pass))
	}
	return nil, err
}

func hostKeyCallback(d config.Device, opts Options) (ssh.HostKeyCallback, error) {
	knownPath := opts.KnownHosts
	if knownPath == "" {
		knownPath = defaultKnownHostsPath()
	}
	if err := os.MkdirAll(filepath.Dir(knownPath), 0o700); err != nil {
		return nil, err
	}
	if _, err := os.Stat(knownPath); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(knownPath, nil, 0o600); err != nil {
			return nil, err
		}
	}
	base, err := knownhosts.New(knownPath)
	if err != nil {
		return nil, err
	}
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := base(hostname, remote, key)
		if err == nil {
			return nil
		}
		var keyErr *knownhosts.KeyError
		if errors.As(err, &keyErr) && len(keyErr.Want) == 0 {
			fingerprint := ssh.FingerprintSHA256(key)
			if opts.ConfirmHost == nil || !opts.ConfirmHost(hostname, fingerprint) {
				return fmt.Errorf("unknown host key for %s (%s) was not trusted", hostname, fingerprint)
			}
			line := knownhosts.Line([]string{hostKeyName(d)}, key)
			f, openErr := os.OpenFile(knownPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
			if openErr != nil {
				return openErr
			}
			defer f.Close()
			if _, writeErr := fmt.Fprintln(f, line); writeErr != nil {
				return writeErr
			}
			return nil
		}
		return err
	}, nil
}

func DetectIdentityFiles() []string {
	keys := defaultKeys()
	var out []string
	for _, key := range keys {
		if st, err := os.Stat(key); err == nil && !st.IsDir() {
			out = append(out, key)
		}
	}
	return out
}

func PromptPassword(prompt string) (string, error) {
	fmt.Fprintf(os.Stderr, "%s: ", prompt)
	b, err := readPassword()
	fmt.Fprintln(os.Stderr)
	return string(b), err
}

func ConfirmHostInteractive(host, fingerprint string) bool {
	fmt.Fprintf(os.Stderr, "The host %s is unknown.\nFingerprint: %s\nTrust this host and add it to known_hosts? [y/N]: ", host, fingerprint)
	r := bufio.NewReader(os.Stdin)
	line, _ := r.ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "y" || line == "yes"
}

func defaultKnownHostsPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".ssh", "known_hosts")
	}
	return filepath.Join(home, ".ssh", "known_hosts")
}

func hostKeyName(d config.Device) string {
	if d.Port == 22 {
		return d.Host
	}
	return fmt.Sprintf("[%s]:%d", d.Host, d.Port)
}

func defaultKeys() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	names := []string{"id_ed25519", "id_ecdsa", "id_rsa"}
	out := make([]string, 0, len(names))
	for _, n := range names {
		out = append(out, filepath.Join(home, ".ssh", n))
	}
	return out
}

func expandHome(p string) string {
	if p == "" || !strings.HasPrefix(p, "~") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	if p == "~" {
		return home
	}
	return filepath.Join(home, strings.TrimPrefix(p, "~/"))
}
