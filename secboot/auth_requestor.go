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

func (r *systemdAuthRequestor) askPassword(sourceDevicePath, msg, credentialName string) (string, error) {
	enableCredential := true
	err := systemd.EnsureAtLeast(249)
	if systemd.IsSystemdTooOld(err) {
		enableCredential = false
	}

	var args []string

	args = append(args, "--icon", "drive-harddisk")
	args = append(args, "--id", filepath.Base(os.Args[0])+":"+sourceDevicePath)

	if enableCredential {
		args = append(args, fmt.Sprintf("--credential=snapd.%s", credentialName))
	}

	args = append(args, msg)

	cmd := exec.Command(
		"systemd-ask-password",
		args...,
	)
	out := new(bytes.Buffer)
	cmd.Stdout = out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("cannot execute systemd-ask-password: %v", err)
	}
	result, err := out.ReadString('\n')
	if err != nil {
		// The only error returned from bytes.Buffer.ReadString is io.EOF.
		return "", errors.New("systemd-ask-password output is missing terminating newline")
	}
	return strings.TrimRight(result, "\n"), nil
}

func (r *systemdAuthRequestor) RequestPassphrase(volumeName, sourceDevicePath string) (string, error) {
	msg := fmt.Sprintf("Please enter the passphrase for volume %s for device %s", volumeName, sourceDevicePath)
	return r.askPassword(sourceDevicePath, msg, "passphrase")
}

func (r *systemdAuthRequestor) RequestRecoveryKey(volumeName, sourceDevicePath string) (sb.RecoveryKey, error) {
	msg := fmt.Sprintf("Please enter the recovery key for volume %s for device %s", volumeName, sourceDevicePath)
	passphrase, err := r.askPassword(sourceDevicePath, msg, "recovery")
	if err != nil {
		return sb.RecoveryKey{}, err
	}

	key, err := sb.ParseRecoveryKey(passphrase)
	if err != nil {
		return sb.RecoveryKey{}, fmt.Errorf("cannot parse recovery key: %w", err)
	}

	return key, nil
}

func NewSystemdAuthRequestor() sb.AuthRequestor {
	return &systemdAuthRequestor{}
}
