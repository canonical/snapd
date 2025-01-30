// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package testutil

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"os/exec"
)

func AppArmorParseAndHashHelper(profile string) (string, error) {
	p, err := exec.LookPath("apparmor_parser")
	if err != nil {
		return "", fmt.Errorf("cannot find apparmor_parser in $PATH: %w", err)
	}
	// Create app_armor parser command with arguments to only return the compiled
	// policy to stdout. The profile is not cached or loaded.
	apparmorParser := exec.Command(p, "-QKS")

	// Get stdin and stdout to pipe the command
	apparmorParserStdin, err := apparmorParser.StdinPipe()
	if err != nil {
		return "", fmt.Errorf("cannot create stdin pipe for apparmor_parser: %w", err)
	}
	apparmorParserStdout, err := apparmorParser.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("cannot create stdout pipe for apparmor_parser: %w", err)
	}

	// Start apparmor_parser command
	if err := apparmorParser.Start(); err != nil {
		return "", fmt.Errorf("cannot start apparmor_parser: %w", err)
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
			return "", fmt.Errorf("apparmor_parser command exited with status code %v", exiterr.ExitCode())
		} else {
			return "", fmt.Errorf("cannot wait() for apparmor_parser process: %w", err)
		}
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}
