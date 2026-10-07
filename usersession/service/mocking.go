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

package service

import "context"

// MockManagers replaces manager discovery and connection establishment for
// callers' tests. Either argument can be nil to leave that boundary unchanged.
func MockManagers(list func(context.Context) ([]int, error), open func(context.Context, int) (Manager, func(), error)) (restore func()) {
	oldDiscover, oldOpen := discover, openManager
	if list != nil {
		discover = list
	}
	if open != nil {
		openManager = open
	}
	return func() { discover, openManager = oldDiscover, oldOpen }
}
