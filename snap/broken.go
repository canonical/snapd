// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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

package snap

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/dirs"
)

func GuessAppsForBroken(info *Info) map[string]*AppInfo {
	out := make(map[string]*AppInfo)

	// guess binaries first
	name := info.SuggestedName
	for _, p := range []string{name, fmt.Sprintf("%s.*", name)} {
		matches, _ := filepath.Glob(filepath.Join(dirs.SnapBinariesDir, p))
		for _, m := range matches {
			l := strings.SplitN(filepath.Base(m), ".", 2)
			var appname string
			if len(l) == 1 {
				appname = l[0]
			} else {
				appname = l[1]
			}
			out[appname] = &AppInfo{
				Snap: info,
				Name: appname,
			}
		}
	}

	// guess the services next
	matches, _ := filepath.Glob(filepath.Join(dirs.SnapServicesDir, fmt.Sprintf("snap.%s.*.service", name)))
	for _, m := range matches {
		appname := strings.Split(m, ".")[2]
		out[appname] = &AppInfo{
			Snap:   info,
			Name:   appname,
			Daemon: "simple",
		}
	}

	return out
}

// renameClashingCorePlugs renames plugs that clash with slot names on core snap.
//
// Some released core snaps had explicitly defined plugs "network-bind" and
// "core-support" that clashed with implicit slots with the same names but this
// was not validated before.  To avoid a flag day and any potential issues,
// transparently rename the two clashing plugs by appending the "-plug" suffix.
func (info *Info) renameClashingCorePlugs() {
	if info.Name() == "core" && info.Type == TypeOS {
		for _, plugName := range []string{"network-bind", "core-support"} {
			info.RenamePlug(plugName, plugName+"-plug")
		}
	}
}
