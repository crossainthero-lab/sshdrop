package devices

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/crossainthero-lab/sshdrop/internal/config"
	sshconfig "github.com/kevinburke/ssh_config"
)

func ImportSSHConfig() ([]config.Device, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(home, ".ssh", "config")
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	cfg, err := sshconfig.Decode(f)
	if err != nil {
		return nil, err
	}
	var out []config.Device
	for _, host := range cfg.Hosts {
		for _, pattern := range host.Patterns {
			name := pattern.String()
			if name == "*" || strings.ContainsAny(name, "*?!") {
				continue
			}
			hostname, _ := cfg.Get(name, "Hostname")
			user, _ := cfg.Get(name, "User")
			portText, _ := cfg.Get(name, "Port")
			identity, _ := cfg.Get(name, "IdentityFile")
			if hostname == "" {
				hostname = name
			}
			port := 22
			if p, err := strconv.Atoi(portText); err == nil && p > 0 {
				port = p
			}
			if user == "" {
				user = os.Getenv("USER")
			}
			identity = expandHome(identity)
			d := config.Device{Name: name, Host: hostname, Port: port, User: user, IdentityFile: identity}
			if config.ValidateDevice(d) == nil {
				out = append(out, d)
			}
		}
	}
	return out, nil
}

func MergeImported(saved []config.Device, imported []config.Device) []config.Device {
	seen := map[string]bool{}
	out := append([]config.Device{}, saved...)
	for _, d := range out {
		seen[strings.ToLower(d.Name)] = true
	}
	for _, d := range imported {
		if !seen[strings.ToLower(d.Name)] {
			out = append(out, d)
			seen[strings.ToLower(d.Name)] = true
		}
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
