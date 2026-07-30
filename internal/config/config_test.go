package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLoadSaveConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sshdrop", "config.yaml")
	cfg := Config{Devices: []Device{{Name: "server", Host: "example.test", User: "william", Port: 2222, IdentityFile: "~/.ssh/id_ed25519"}}}
	if err := Save(path, cfg); err != nil {
		t.Fatalf("save: %v", err)
	}
	st, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat config dir: %v", err)
	}
	if runtime.GOOS != "windows" && st.Mode().Perm() != 0o700 {
		t.Fatalf("config dir permissions = %o", st.Mode().Perm())
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded.Devices) != 1 || loaded.Devices[0].Port != 2222 {
		t.Fatalf("unexpected config: %+v", loaded)
	}
}

func TestValidateDevice(t *testing.T) {
	valid := Device{Name: "pi", Host: "192.0.2.10", User: "william", Port: 22}
	if err := ValidateDevice(valid); err != nil {
		t.Fatalf("valid device rejected: %v", err)
	}
	invalid := []Device{
		{Name: "", Host: "host", User: "u", Port: 22},
		{Name: "bad:name", Host: "host", User: "u", Port: 22},
		{Name: "x", Host: "", User: "u", Port: 22},
		{Name: "x", Host: "host", User: "", Port: 22},
		{Name: "x", Host: "host", User: "u", Port: 70000},
	}
	for _, d := range invalid {
		if err := ValidateDevice(d); err == nil {
			t.Fatalf("invalid device accepted: %+v", d)
		}
	}
}

func TestFindRemoveDevice(t *testing.T) {
	cfg := Config{}
	cfg.UpsertDevice(Device{Name: "MacBook", Host: "m", User: "u", Port: 22})
	cfg.UpsertDevice(Device{Name: "macbook", Host: "m2", User: "u", Port: 22})
	if len(cfg.Devices) != 1 || cfg.Devices[0].Host != "m2" {
		t.Fatalf("upsert failed: %+v", cfg.Devices)
	}
	if _, ok := cfg.FindDevice("MACBOOK"); !ok {
		t.Fatal("case-insensitive find failed")
	}
	if !cfg.RemoveDevice("macbook") || len(cfg.Devices) != 0 {
		t.Fatal("remove failed")
	}
}
