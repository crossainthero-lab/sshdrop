package devices

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/crossainthero-lab/sshdrop/internal/config"
)

func TestMergeImportedKeepsSavedProfiles(t *testing.T) {
	saved := []config.Device{{Name: "server", Host: "saved", User: "me", Port: 22}}
	imported := []config.Device{{Name: "server", Host: "imported", User: "me", Port: 22}, {Name: "pi", Host: "pi.local", User: "me", Port: 2222}}
	got := MergeImported(saved, imported)
	if len(got) != 2 {
		t.Fatalf("expected 2 devices, got %d", len(got))
	}
	if got[0].Host != "saved" {
		t.Fatalf("saved profile was overwritten: %+v", got[0])
	}
}

func TestImportSSHConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	data := []byte("Host lab\n  HostName 192.0.2.5\n  User will\n  Port 2200\n  IdentityFile ~/.ssh/id_ed25519\n\nHost *\n  User ignored\n")
	if err := os.WriteFile(filepath.Join(sshDir, "config"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	devices, err := ImportSSHConfig()
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if len(devices) != 1 {
		t.Fatalf("expected one imported device, got %+v", devices)
	}
	if devices[0].Name != "lab" || devices[0].Host != "192.0.2.5" || devices[0].Port != 2200 || devices[0].User != "will" {
		t.Fatalf("bad imported device: %+v", devices[0])
	}
}
