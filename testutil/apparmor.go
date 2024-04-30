package testutil

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"os/exec"
)

func AppArmorParseAndHashHelper(profile string) (string, error) {
	// Create app_armor parser command with arguments to only return the compiled
	// policy to stdout. The profile is not cached or loaded.
	apparmorParser := exec.Command("apparmor_parser", "-QKS")

	// Get stdin and stdout to pipe the command
	apparmorParserStdin, err := apparmorParser.StdinPipe()
	if err != nil {
		return "Error creating stdin pipe for apparmor_parser", err
	}
	apparmorParserStdout, err := apparmorParser.StdoutPipe()
	if err != nil {
		return "Error creating stdout pipe for apparmor_parser", err
	}

	// Start apparmor_parser command
	if err := apparmorParser.Start(); err != nil {
		return "Error starting apparmor_parser", err
	}

	// Write apparmor profile to apparmor_parser stdin
	go func() {
		defer apparmorParserStdin.Close()
		io.WriteString(apparmorParserStdin, profile)
	}()

	// Calculate the hash
	h := sha1.New()
	io.Copy(h, apparmorParserStdout)

	// Get apparmor_parser command output
	if err := apparmorParser.Wait(); err != nil {
		if exiterr, ok := err.(*exec.ExitError); ok {
			return fmt.Sprintf("apparmor_parser command exited with status code %d", exiterr.ExitCode()), err
		} else {
			return "Error waiting for apparmor_parser command", err
		}
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}
