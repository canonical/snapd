// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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
	"time"

	"github.com/snapcore/snapd/systemd"
)

func MockSystemd(string, systemd.Notifier) systemd.Systemd {
	return SystemdInstance
}

type Systemd struct {
	Ops       [][]string
	Err       error
	SvcStatus *systemd.ServiceStatus
	LogList   []systemd.Log
}

var _ systemd.Systemd = (*Systemd)(nil)

var SystemdInstance *Systemd

func ResetSystemd() {
	SystemdInstance = &Systemd{}
}

func (s *Systemd) add(op string, args []string) error {
	opArgs := make([]string, len(args)+1)
	opArgs[0] = op
	copy(opArgs[1:], args)
	s.Ops = append(s.Ops, opArgs)
	return s.Err
}

func (s *Systemd) DaemonReload() error                 { return s.add("daemon-reload", nil) }
func (s *Systemd) Enable(services ...string) error     { return s.add("enable", services) }
func (s *Systemd) EnableNow(services ...string) error  { return s.add("enable-now", services) }
func (s *Systemd) Disable(services ...string) error    { return s.add("disable", services) }
func (s *Systemd) DisableNow(services ...string) error { return s.add("disable-now", services) }
func (s *Systemd) Start(services ...string) error      { return s.add("start", services) }
func (s *Systemd) Stop(services ...string) error       { return s.add("stop", services) }
func (s *Systemd) Restart(services ...string) error    { return s.add("restart", services) }
func (s *Systemd) Reload(services ...string) error     { return s.add("reload", services) }
func (s *Systemd) Kill(service, signal string) error   { return s.add("kill", []string{service, signal}) }
func (s *Systemd) Logs(services []string) ([]systemd.Log, error) {
	err := s.add("logs", services)
	return s.LogList, err
}
func (s *Systemd) Status(service string) (string, error) {
	err := s.add("status", []string{service})
	return s.SvcStatus.String(), err
}
func (s *Systemd) ServiceStatus(service string) (*systemd.ServiceStatus, error) {
	err := s.add("service-status", []string{service})
	return s.SvcStatus, err
}
func (s *Systemd) StopAndWait(service string, timeout time.Duration) error {
	return s.add("stop-and-wait", []string{service, timeout.String()})
}
func (s *Systemd) RestartAndWait(service string, timeout time.Duration) error {
	return s.add("restart-and-wait", []string{service, timeout.String()})
}
func (s *Systemd) WriteMountUnitFile(name, what, where, fstype string) (string, error) {
	panic("not implemented")
}
