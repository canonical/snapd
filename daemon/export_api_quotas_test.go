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

package daemon

import (
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/overlord/servicestate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap/quota"
)

type (
	PostQuotaGroupData = postQuotaGroupData
)

func MockServicestateCreateQuota(f func(st *state.State, name string, parentName string, snaps []string, resourceLimits quota.Resources) (*state.TaskSet, error)) func() {
	old := servicestateCreateQuota
	servicestateCreateQuota = f
	return func() {
		servicestateCreateQuota = old
	}
}

func MockServicestateUpdateQuota(f func(st *state.State, name string, opts servicestate.QuotaGroupUpdate) (*state.TaskSet, error)) func() {
	old := servicestateUpdateQuota
	servicestateUpdateQuota = f
	return func() {
		servicestateUpdateQuota = old
	}
}

func MockServicestateRemoveQuota(f func(st *state.State, name string) (*state.TaskSet, error)) func() {
	old := servicestateRemoveQuota
	servicestateRemoveQuota = f
	return func() {
		servicestateRemoveQuota = old
	}
}

func MockGetQuotaUsage(f func(grp *quota.Group) (*client.QuotaValues, error)) (restore func()) {
	old := getQuotaUsage
	getQuotaUsage = f
	return func() {
		getQuotaUsage = old
	}
}
