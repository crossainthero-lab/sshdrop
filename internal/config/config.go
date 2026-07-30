package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const AppName = "sshdrop"

type Config struct {
	Devices []Device `yaml:"devices"`
}

type Device struct {
	Name             string `yaml:"name"`
	Host             string `yaml:"host"`
	Port             int    `yaml:"port"`
	User             string `yaml:"user"`
	IdentityFile     string `yaml:"identity_file,omitempty"`
	DefaultRemoteDir string `yaml:"default_remote_dir,omitempty"`
	DefaultLocalDir  string `yaml:"default_local_dir,omitempty"`
}

func DefaultPath() (string, error) {
	base := os.Getenv("SSHDROP_CONFIG_HOME")
	if base == "" {
		if runtime.GOOS == "darwin" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			base = filepath.Join(home, "Library", "Application Support")
		} else if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			base = xdg
		} else {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			base = filepath.Join(home, ".config")
		}
	}
	return filepath.Join(base, AppName, "config.yaml"), nil
}

func Load(path string) (Config, error) {
	if path == "" {
		var err error
		path, err = DefaultPath()
		if err != nil {
			return Config{}, err
		}
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("invalid configuration file %s: %w", path, err)
	}
	for i := range cfg.Devices {
		if cfg.Devices[i].Port == 0 {
			cfg.Devices[i].Port = 22
		}
		cfg.Devices[i].Name = strings.TrimSpace(cfg.Devices[i].Name)
	}
	return cfg, nil
}

func Save(path string, cfg Config) error {
	if path == "" {
		var err error
		path, err = DefaultPath()
		if err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	normalized := cfg
	sort.SliceStable(normalized.Devices, func(i, j int) bool {
		return strings.ToLower(normalized.Devices[i].Name) < strings.ToLower(normalized.Devices[j].Name)
	})
	data, err := yaml.Marshal(normalized)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func (c Config) FindDevice(name string) (Device, bool) {
	for _, d := range c.Devices {
		if strings.EqualFold(d.Name, name) {
			return d, true
		}
	}
	return Device{}, false
}

func (c *Config) UpsertDevice(d Device) {
	for i := range c.Devices {
		if strings.EqualFold(c.Devices[i].Name, d.Name) {
			c.Devices[i] = d
			return
		}
	}
	c.Devices = append(c.Devices, d)
}

func (c *Config) RemoveDevice(name string) bool {
	for i := range c.Devices {
		if strings.EqualFold(c.Devices[i].Name, name) {
			c.Devices = append(c.Devices[:i], c.Devices[i+1:]...)
			return true
		}
	}
	return false
}

func ValidateDevice(d Device) error {
	if strings.TrimSpace(d.Name) == "" {
		return errors.New("device name is required")
	}
	if strings.ContainsAny(d.Name, ":/\r\n\t") {
		return errors.New("device name cannot contain ':', '/', tabs, or newlines")
	}
	if strings.TrimSpace(d.Host) == "" {
		return errors.New("hostname or IP address is required")
	}
	if strings.TrimSpace(d.User) == "" {
		return errors.New("SSH username is required")
	}
	if d.Port <= 0 || d.Port > 65535 {
		return errors.New("SSH port must be between 1 and 65535")
	}
	return nil
}
