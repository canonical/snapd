// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nosecboot

/*
 * Copyright (C) 2025 Canonical Ltd
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

package secboot

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	sb "github.com/snapcore/secboot"

	"github.com/snapcore/snapd/systemd"
)

type systemdAuthRequestor struct {
}

func getAskPasswordMessage(authType sb.UserAuthType) (string, error) {
	switch authType {
	case sb.UserAuthTypePassphrase:
		return "Enter passphrase for %[1]s (%[2]s):", nil
	case sb.UserAuthTypePIN:
		return "Enter PIN for %[1]s (%[2]s):", nil
	case sb.UserAuthTypeRecoveryKey:
		return "Enter recovery key for %[1]s (%[2]s):", nil
	case sb.UserAuthTypePassphrase | sb.UserAuthTypePIN:
		return "Enter passphrase or PIN for %[1]s (%[2]s):", nil
	case sb.UserAuthTypePassphrase | sb.UserAuthTypeRecoveryKey:
		return "Enter passphrase or recovery key for %[1]s (%[2]s):", nil
	case sb.UserAuthTypePIN | sb.UserAuthTypeRecoveryKey:
		return "Enter PIN or recovery key for %[1]s (%[2]s):", nil
	case sb.UserAuthTypePassphrase | sb.UserAuthTypePIN | sb.UserAuthTypeRecoveryKey:
		return "Enter passphrase, PIN or recovery key for %[1]s (%[2]s):", nil
	default:
		return "", errors.New("unexpected UserAuthType")
	}
}

// RequestUserCredential implements AuthRequestor.RequestUserCredential
func (r *systemdAuthRequestor) RequestUserCredential(ctx context.Context, name, path string, authTypes sb.UserAuthType) (string, error) {
	fmtMessage, err := getAskPasswordMessage(authTypes)
	if err != nil {
		return "", err
	}

	enableCredential := true
	err = systemd.EnsureAtLeast(249)
	if systemd.IsSystemdTooOld(err) {
		enableCredential = false
	}

	var args []string

	args = append(args, "--icon", "drive-harddisk")
	args = append(args, "--id", filepath.Base(os.Args[0])+":"+path)

	if enableCredential {
		args = append(args, "--credential=snapd.fde.password")
	}

	msg := fmt.Sprintf(fmtMessage, name, path)
	args = append(args, msg)

	cmd := exec.CommandContext(
		ctx, "systemd-ask-password",
		args...)
	out := new(bytes.Buffer)
	cmd.Stdout = out
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("cannot execute systemd-ask-password: %w", err)
	}
	result, err := out.ReadString('\n')
	if err != nil {
		// The only error returned from bytes.Buffer.ReadString is io.EOF.
		return "", errors.New("systemd-ask-password output is missing terminating newline")
	}
	return strings.TrimRight(result, "\n"), nil
}

// NewSystemdAuthRequestor creates an AuthRequestor
// which calls systemd-ask-password with credential parameter.
func NewSystemdAuthRequestor() sb.AuthRequestor {
	return &systemdAuthRequestor{}
}
