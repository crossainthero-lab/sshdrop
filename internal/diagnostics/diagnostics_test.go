package diagnostics

import (
	"errors"
	"net"
	"strings"
	"testing"
)

func TestTranslateDNSError(t *testing.T) {
	msg := Translate(&net.DNSError{Name: "badhost"})
	if !strings.Contains(msg, "DNS") {
		t.Fatalf("unexpected message: %s", msg)
	}
}

func TestTranslateAuthError(t *testing.T) {
	msg := Translate(errors.New("ssh: unable to authenticate, attempted methods [none]"))
	if !strings.Contains(msg, "authentication failed") {
		t.Fatalf("unexpected message: %s", msg)
	}
}
