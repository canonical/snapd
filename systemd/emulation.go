// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package systemd

import (
	"errors"
	"io"
	"time"
)

type emulation struct {
	rootDir  string
	reporter reporter
}

var errNotImplemented = errors.New("not implemented in emulation mode")

func (s *emulation) DaemonReload() error {
	return errNotImplemented
}

func (s *emulation) Enable(service string) error {
	return errNotImplemented
}

func (s *emulation) Disable(service string) error {
	return errNotImplemented
}

func (s *emulation) Start(service ...string) error {
	return errNotImplemented
}

func (s *emulation) StartNoBlock(service ...string) error {
	return errNotImplemented
}

func (s *emulation) Stop(service string, timeout time.Duration) error {
	return errNotImplemented
}

func (s *emulation) Kill(service, signal, who string) error {
	return errNotImplemented
}

func (s *emulation) Restart(service string, timeout time.Duration) error {
	return errNotImplemented
}

func (s *emulation) Status(units ...string) ([]*UnitStatus, error) {
	return nil, errNotImplemented
}

func (s *emulation) IsEnabled(service string) (bool, error) {
	return false, errNotImplemented
}

func (s *emulation) IsActive(service string) (bool, error) {
	return false, errNotImplemented
}

func (s *emulation) LogReader(services []string, n int, follow bool) (io.ReadCloser, error) {
	return nil, errNotImplemented
}

func (s *emulation) AddMountUnitFile(name, revision, what, where, fstype string) (string, error) {
	return "", errNotImplemented
}

func (s *emulation) RemoveMountUnitFile(baseDir string) error {
	return errNotImplemented
}

func (s *emulation) Mask(service string) error {
	return errNotImplemented
}

func (s *emulation) Unmask(service string) error {
	return errNotImplemented
}
