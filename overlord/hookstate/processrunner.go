// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package hookstate

import (
	"bytes"
	"os/exec"
)

type ProcessRunner interface {
	// Kill kills the running process (if any)
	Kill() error

	// Wait waits for the running process to complete, and returns combined
	// stdout/stderr.
	Wait() ([]byte, error)
}

type execProcessRunner struct {
	command *exec.Cmd
	buffer  *bytes.Buffer
}

// newProcessRunner starts a new process with the requested parameters and
// returns its ProcessRunner.
func newProcessRunner(name string, args ...string) (ProcessRunner, error) {
	command := exec.Command(name, args...)

	// Make sure we can obtain stdout and stderror. Same buffer so they're
	// combined.
	buffer := bytes.NewBuffer(nil)
	command.Stdout = buffer
	command.Stderr = buffer

	if err := command.Start(); err != nil {
		return nil, err
	}

	return &execProcessRunner{command: command, buffer: buffer}, nil
}

func (r *execProcessRunner) Kill() error {
	return r.command.Process.Kill()
}

func (r *execProcessRunner) Wait() ([]byte, error) {
	if err := r.command.Wait(); err != nil {
		return nil, err
	}

	return r.buffer.Bytes(), nil
}
