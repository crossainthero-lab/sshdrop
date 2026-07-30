package app

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/crossainthero-lab/sshdrop/internal/config"
	"github.com/crossainthero-lab/sshdrop/internal/connection"
	"github.com/crossainthero-lab/sshdrop/internal/devices"
	"github.com/crossainthero-lab/sshdrop/internal/diagnostics"
	"github.com/crossainthero-lab/sshdrop/internal/filesystem"
	"github.com/crossainthero-lab/sshdrop/internal/transfer"
	"github.com/crossainthero-lab/sshdrop/internal/tui"
)

const Version = "0.1.0"

type options struct {
	verbose bool
}

func Run(args []string) error {
	opts, rest, err := parseGlobal(args)
	if err != nil {
		return err
	}
	cfg, err := loadConfigWithImports()
	if err != nil {
		return errors.New(diagnostics.Translate(err))
	}
	if len(rest) == 0 {
		return tui.Run(cfg, "", opts.verbose)
	}
	switch rest[0] {
	case "connect":
		if len(rest) != 2 {
			return errors.New("usage: sshdrop connect <device>")
		}
		return tui.Run(cfg, rest[1], opts.verbose)
	case "upload":
		return runUpload(cfg, opts, rest[1:])
	case "download":
		return runDownload(cfg, opts, rest[1:])
	case "devices":
		return listDevices(cfg)
	case "device":
		return runDeviceCommand(cfg, opts, rest[1:])
	case "doctor":
		return doctor(cfg)
	case "version", "--version", "-v":
		fmt.Printf("sshdrop %s\n", Version)
		return nil
	case "help", "--help", "-h":
		usage()
		return nil
	default:
		return fmt.Errorf("unknown command %q\n\nRun sshdrop help for usage.", rest[0])
	}
}

func parseGlobal(args []string) (options, []string, error) {
	fs := flag.NewFlagSet("sshdrop", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var opts options
	fs.BoolVar(&opts.verbose, "verbose", false, "show verbose diagnostics")
	if err := fs.Parse(args); err != nil {
		return opts, nil, err
	}
	return opts, fs.Args(), nil
}

func loadConfigWithImports() (config.Config, error) {
	cfg, err := config.Load("")
	if err != nil {
		return config.Config{}, err
	}
	imported, err := devices.ImportSSHConfig()
	if err == nil {
		cfg.Devices = devices.MergeImported(cfg.Devices, imported)
	}
	return cfg, nil
}

func listDevices(cfg config.Config) error {
	if len(cfg.Devices) == 0 {
		fmt.Println("No saved devices. Add one with: sshdrop device add")
		return nil
	}
	for _, d := range cfg.Devices {
		identity := "agent/default keys"
		if d.IdentityFile != "" {
			identity = d.IdentityFile
		}
		fmt.Printf("%-20s %s@%s:%d  key=%s\n", d.Name, d.User, d.Host, d.Port, identity)
	}
	return nil
}

func runDeviceCommand(cfg config.Config, opts options, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: sshdrop device <add|remove|test>")
	}
	switch args[0] {
	case "add":
		return addDeviceWizard(cfg, opts)
	case "remove":
		if len(args) != 2 {
			return errors.New("usage: sshdrop device remove <name>")
		}
		if !cfg.RemoveDevice(args[1]) {
			return fmt.Errorf("device %q was not found", args[1])
		}
		if err := config.Save("", cfg); err != nil {
			return err
		}
		fmt.Printf("Removed device %s\n", args[1])
		return nil
	case "test":
		if len(args) != 2 {
			return errors.New("usage: sshdrop device test <name>")
		}
		d, ok := cfg.FindDevice(args[1])
		if !ok {
			return fmt.Errorf("device %q was not found", args[1])
		}
		return testDevice(d, opts)
	default:
		return fmt.Errorf("unknown device command %q", args[0])
	}
}

func addDeviceWizard(cfg config.Config, opts options) error {
	in := bufio.NewReader(os.Stdin)
	fmt.Println("SSHDrop device setup")
	name := prompt(in, "Device name")
	host := prompt(in, "Hostname or IP")
	user := prompt(in, "SSH username")
	portText := promptDefault(in, "SSH port", "22")
	port, err := strconv.Atoi(portText)
	if err != nil {
		return fmt.Errorf("invalid port %q", portText)
	}
	keys := connection.DetectIdentityFiles()
	fmt.Println("Authentication method:")
	fmt.Println("  0) SSH agent / default keys")
	for i, key := range keys {
		fmt.Printf("  %d) %s\n", i+1, key)
	}
	choice := promptDefault(in, "Select key", "0")
	identity := ""
	if n, err := strconv.Atoi(choice); err == nil && n > 0 && n <= len(keys) {
		identity = keys[n-1]
	}
	d := config.Device{Name: name, Host: host, User: user, Port: port, IdentityFile: identity}
	if err := config.ValidateDevice(d); err != nil {
		return err
	}
	fmt.Println("Testing connection...")
	if err := testDevice(d, opts); err != nil {
		return err
	}
	remote := promptDefault(in, "Default remote directory", ".")
	local := promptDefault(in, "Default local directory", ".")
	if remote == "." || remote == "" {
		remote = ""
	}
	if local == "." || local == "" {
		local = ""
	} else if clean, err := filesystem.ValidateLocalPath(local); err == nil {
		local = clean
	}
	d.DefaultRemoteDir = remote
	d.DefaultLocalDir = local
	cfg.UpsertDevice(d)
	if err := config.Save("", cfg); err != nil {
		return err
	}
	fmt.Printf("Saved device %s\n", d.Name)
	return nil
}

func testDevice(d config.Device, opts options) error {
	c, err := connection.Dial(d, dialOptions(opts))
	if err != nil {
		return fmt.Errorf("could not connect to %s: %s", d.Name, diagnostics.Translate(err))
	}
	defer c.Close()
	wd, err := c.SFTP.Getwd()
	if err != nil {
		return fmt.Errorf("connected, but SFTP failed: %s", diagnostics.Translate(err))
	}
	fmt.Printf("Connected to %s. Remote directory: %s\n", d.Name, wd)
	return nil
}

func runUpload(cfg config.Config, opts options, args []string) error {
	if len(args) < 2 {
		return errors.New("usage: sshdrop upload <local-path> [local-path...] <device:/remote/dir>")
	}
	target, err := parseTarget(cfg, args[len(args)-1], true)
	if err != nil {
		return err
	}
	c, err := connection.Dial(target.Device, dialOptions(opts))
	if err != nil {
		return fmt.Errorf("upload connection failed: %s", diagnostics.Translate(err))
	}
	defer c.Close()
	m := transfer.NewManager()
	m.SetConflictResolver(conflictPrompt)
	_, err = m.EnqueueUpload(context.Background(), c.SFTP, args[:len(args)-1], target.Path)
	if err != nil {
		return errors.New(diagnostics.Translate(err))
	}
	return waitForTransfers(m)
}

func runDownload(cfg config.Config, opts options, args []string) error {
	if len(args) != 2 {
		return errors.New("usage: sshdrop download <device:/remote/path> <local-dir>")
	}
	target, err := parseTarget(cfg, args[0], false)
	if err != nil {
		return err
	}
	localDir, err := filesystem.ValidateLocalPath(args[1])
	if err != nil {
		return err
	}
	c, err := connection.Dial(target.Device, dialOptions(opts))
	if err != nil {
		return fmt.Errorf("download connection failed: %s", diagnostics.Translate(err))
	}
	defer c.Close()
	m := transfer.NewManager()
	m.SetConflictResolver(conflictPrompt)
	_, err = m.EnqueueDownload(context.Background(), c.SFTP, []string{target.Path}, localDir)
	if err != nil {
		return errors.New(diagnostics.Translate(err))
	}
	return waitForTransfers(m)
}

type target struct {
	Device config.Device
	Path   string
}

func parseTarget(cfg config.Config, spec string, destination bool) (target, error) {
	idx := strings.Index(spec, ":")
	if idx <= 0 {
		return target{}, fmt.Errorf("target must be device:/path or user@host:/path: %s", spec)
	}
	left, remote := spec[:idx], spec[idx+1:]
	if remote == "" {
		remote = "."
	}
	if !strings.HasPrefix(remote, "/") && remote != "." {
		return target{}, fmt.Errorf("remote path must be absolute: %s", remote)
	}
	if remote == "." {
		remote = "/"
	}
	if d, ok := cfg.FindDevice(left); ok {
		return target{Device: d, Path: path.Clean(remote)}, nil
	}
	if strings.Contains(left, "@") {
		parts := strings.SplitN(left, "@", 2)
		host := parts[1]
		port := 22
		if h, p, ok := strings.Cut(host, ":"); ok {
			host = h
			if parsed, err := strconv.Atoi(p); err == nil {
				port = parsed
			}
		}
		d := config.Device{Name: left, Host: host, Port: port, User: parts[0]}
		if err := config.ValidateDevice(d); err != nil {
			return target{}, err
		}
		return target{Device: d, Path: path.Clean(remote)}, nil
	}
	return target{}, fmt.Errorf("unknown device %q; run sshdrop devices or sshdrop device add", left)
}

func dialOptions(opts options) connection.Options {
	return connection.Options{
		Verbose:       opts.verbose,
		Password:      connection.PromptPassword,
		ConfirmHost:   connection.ConfirmHostInteractive,
		AllowPassword: true,
		Timeout:       15 * time.Second,
	}
}

func conflictPrompt(dst string, _ transfer.Direction) transfer.ConflictAction {
	in := bufio.NewReader(os.Stdin)
	fmt.Printf("Destination exists: %s\n[o]verwrite, [s]kip, [r]ename? ", dst)
	answer, _ := in.ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "s", "skip":
		return transfer.ConflictSkip
	case "r", "rename":
		return transfer.ConflictRename
	default:
		return transfer.ConflictOverwrite
	}
}

func waitForTransfers(m *transfer.Manager) error {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for range ticker.C {
		snap := m.Snapshot()
		if snap.Active != nil {
			pct := 0.0
			if snap.Active.Size > 0 {
				pct = float64(snap.Active.Transferred) / float64(snap.Active.Size) * 100
			}
			fmt.Printf("\r%s %s %.0f%% %s/s", snap.Active.Direction, filepath.Base(snap.Active.Source), pct, filesystem.FormatSize(int64(snap.Speed)))
		}
		if snap.Queued == 0 && snap.Active == nil {
			fmt.Println()
			if snap.Failed > 0 {
				for _, j := range snap.Jobs {
					if j.Status == "failed" || j.Status == "cancelled" {
						return fmt.Errorf("%s failed: %s", j.Source, diagnostics.Translate(errors.New(j.Error)))
					}
				}
			}
			fmt.Printf("Completed %d transfer(s).\n", snap.Completed)
			return nil
		}
	}
	return nil
}

func doctor(cfg config.Config) error {
	configPath, _ := config.DefaultPath()
	fmt.Printf("SSHDrop %s\n", Version)
	fmt.Printf("Config: %s\n", configPath)
	fmt.Printf("Devices: %d\n", len(cfg.Devices))
	fmt.Printf("SSH_AUTH_SOCK: %s\n", redactedEnv("SSH_AUTH_SOCK"))
	fmt.Println("Detected identity files:")
	for _, key := range connection.DetectIdentityFiles() {
		fmt.Printf("  %s\n", key)
	}
	return nil
}

func redactedEnv(key string) string {
	if os.Getenv(key) == "" {
		return "(not set)"
	}
	return "(set)"
}

func prompt(in *bufio.Reader, label string) string {
	fmt.Printf("%s: ", label)
	line, _ := in.ReadString('\n')
	return strings.TrimSpace(line)
}

func promptDefault(in *bufio.Reader, label, def string) string {
	fmt.Printf("%s [%s]: ", label, def)
	line, _ := in.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return def
	}
	return line
}

func usage() {
	fmt.Println(`SSHDrop - terminal SFTP file transfer

Usage:
  sshdrop
  sshdrop connect <device>
  sshdrop upload <local-path> [local-path...] <device:/remote/dir>
  sshdrop download <device:/remote/path> <local-dir>
  sshdrop devices
  sshdrop device add
  sshdrop device remove <device>
  sshdrop device test <device>
  sshdrop doctor
  sshdrop version

Global flags:
  --verbose`)
}
