//go:build windows

package connection

import (
	"os"

	"golang.org/x/term"
)

func readPassword() ([]byte, error) {
	return term.ReadPassword(int(os.Stdin.Fd()))
}
