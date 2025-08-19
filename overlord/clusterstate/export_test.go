// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

package clusterstate

import (
	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/overlord/state"
)

// export for tests
var (
	TaskAssembleClusterSetup = taskAssembleClusterSetup
)

// AssembleClusterSetup is exported for tests
type AssembleClusterSetup = assembleClusterSetup

// DoAssembleCluster is exported for tests
func (m *ClusterManager) DoAssembleCluster(t *state.Task, tb *tomb.Tomb) error {
	return m.doAssembleCluster(t, tb)
}
