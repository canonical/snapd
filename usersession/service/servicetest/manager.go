// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program. If not, see <http://www.gnu.org/licenses/>.
 */

// Package servicetest adapts existing systemd command expectations to the user
// manager boundary. It is only for tests; production operations use D-Bus.
package servicetest

import (
	"context"

	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/systemd"
	systemduser "github.com/snapcore/snapd/systemd/user"
	"github.com/snapcore/snapd/usersession/service"
)

type manager struct{ sysd systemd.Systemd }

func (m manager) Start(_ context.Context, unit string) error { return m.sysd.Start([]string{unit}) }
func (m manager) Stop(_ context.Context, unit string) error  { return m.sysd.Stop([]string{unit}) }
func (m manager) ReloadOrRestart(_ context.Context, unit string) error {
	return m.sysd.ReloadOrRestart([]string{unit})
}
func (m manager) Reload(_ context.Context) error { return m.sysd.DaemonReload() }
func (m manager) Enable(_ context.Context, units []string) ([]systemduser.UnitFileChange, error) {
	return nil, m.sysd.EnableNoReload(units)
}
func (m manager) Disable(_ context.Context, units []string) error {
	return m.sysd.DisableNoReload(units)
}
func (m manager) Status(_ context.Context, units []string) ([]*systemd.UnitStatus, error) {
	var result []*systemd.UnitStatus
	for _, unit := range units {
		sts, err := m.sysd.Status([]string{unit})
		if err != nil {
			return nil, err
		}
		result = append(result, sts...)
	}
	return result, nil
}

// MockSystemd selects fake managers for the given UIDs. With no UIDs it selects
// no managers. It preserves systemctl expectations while exercising root-side
// orchestration; discovery and D-Bus protocol behavior have independent tests.
func MockSystemd(uids ...int) func() {
	available := append([]int(nil), uids...)
	return service.MockManagers(func(context.Context) ([]int, error) { return available, nil }, func(context.Context, int) (service.Manager, func(), error) {
		return manager{systemd.New(systemd.UserMode, progress.Null)}, func() {}, nil
	})
}
