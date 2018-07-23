// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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

package ifacerepo

import (
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/overlord/state"
)

type interfacesRepoKey struct{}

// Replace replaces the interface repository used by the managers.
func Replace(st *state.State, repo *interfaces.Repository) {
	st.Cache(interfacesRepoKey{}, repo)
}

// Get returns the interface repository used by the managers.
func Get(st *state.State) *interfaces.Repository {
	repo := st.Cached(interfacesRepoKey{})
	if repo == nil {
		panic("internal error: cannot find cached interfaces repository, interface manager not initialized?")
	}
	return repo.(*interfaces.Repository)
}
