// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

package servicestate

import (
	"errors"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/overlord/servicestate/internal"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap/quota"
)

var ErrQuotaNotFound = errors.New("quota not found")

// AllQuotas returns all currently tracked quota groups in the state. They are
// validated for consistency using ResolveCrossReferences before being returned.
func AllQuotas(st *state.State) (map[string]*quota.Group, error) {
	return internal.AllQuotas(st)
}

// GetQuota returns an individual quota group by name.
func GetQuota(st *state.State, name string) (*quota.Group, error) {
	allGrps := mylog.Check2(internal.AllQuotas(st))

	group, ok := allGrps[name]
	if !ok {
		return nil, ErrQuotaNotFound
	}

	return group, nil
}
