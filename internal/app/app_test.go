package app

import (
	"testing"

	"github.com/crossainthero-lab/sshdrop/internal/config"
)

func TestParseTargetDevice(t *testing.T) {
	cfg := config.Config{Devices: []config.Device{{Name: "server", Host: "example.test", User: "will", Port: 22}}}
	got, err := parseTarget(cfg, "server:/var/log", false)
	if err != nil {
		t.Fatalf("parse target: %v", err)
	}
	if got.Device.Host != "example.test" || got.Path != "/var/log" {
		t.Fatalf("bad target: %+v", got)
	}
}

func TestParseTargetUserHost(t *testing.T) {
	got, err := parseTarget(config.Config{}, "will@example.test:/tmp", false)
	if err != nil {
		t.Fatalf("parse target: %v", err)
	}
	if got.Device.User != "will" || got.Device.Host != "example.test" || got.Device.Port != 22 || got.Path != "/tmp" {
		t.Fatalf("bad target: %+v", got)
	}
}

func TestParseTargetRejectsRelativeRemotePath(t *testing.T) {
	if _, err := parseTarget(config.Config{}, "will@example.test:tmp", false); err == nil {
		t.Fatal("relative path accepted")
	}
}
