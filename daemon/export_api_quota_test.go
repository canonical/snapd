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

import "github.com/snapcore/snapd/gadget/quantity"

type (
	PostQuotaGroupData   = postQuotaGroupData
	QuotaGroupResultJSON = quotaGroupResultJSON
)

func MockServicestateCreateQuota(f func(name string, parentName string, snaps []string, memoryLimit quantity.Size) error) func() {
	old := servicestateCreateQuota
	servicestateCreateQuota = f
	return func() {
		servicestateCreateQuota = old
	}
}

func MockServicestateRemoveQuota(f func(name string) error) func() {
	old := servicestateRemoveQuota
	servicestateRemoveQuota = f
	return func() {
		servicestateRemoveQuota = old
	}
}
