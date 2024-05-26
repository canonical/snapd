// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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
	"fmt"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
)

var snapstateMigrateHome = snapstate.MigrateHome

func migrateHome(st *state.State, snaps []string) Response {
	if len(snaps) == 0 {
		return BadRequest("no snaps were provided")
	}

	tss := mylog.Check2(snapstateMigrateHome(st, snaps))

	chg := st.NewChange("migrate-home", fmt.Sprintf("Migrate snap homes to ~/Snap for snaps %s", strutil.Quoted(snaps)))
	for _, ts := range tss {
		chg.AddAll(ts)
	}
	chg.Set("api-data", map[string][]string{"snap-names": snaps})

	ensureStateSoon(st)
	return AsyncResponse(nil, chg.ID())
}
