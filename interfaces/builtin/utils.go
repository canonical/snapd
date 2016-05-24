// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package builtin

import (
	"bytes"
	"fmt"
	"sort"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/snap"
)

// AppLabelExpr returns the specification of the apparmor label describing
// all the apps bound to a given slot. The result has one of three forms,
// depending on how apps are bound to the slot:
//
// - "snap.$snap.$app" if there is exactly one app bound
// - "snap.$snap.{$app1,...$appN}" if there are some, but not all, apps bound
// - "snap.$snap.*" if all apps are bound to the slot
func appLabelExpr(apps map[string]*snap.AppInfo, snap *snap.Info) []byte {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, `"snap.%s.`, snap.Name())
	if len(apps) == 1 {
		for appName := range apps {
			buf.WriteString(appName)
		}
	} else if len(apps) == len(snap.Apps) {
		buf.WriteByte('*')
	} else {
		appNames := make([]string, 0, len(apps))
		for appName := range apps {
			appNames = append(appNames, appName)
		}
		sort.Strings(appNames)
		buf.WriteByte('{')
		for _, appName := range appNames {
			buf.WriteString(appName)
			buf.WriteByte(',')
		}
		buf.Truncate(buf.Len() - 1)
		buf.WriteByte('}')
	}
	buf.WriteByte('"')
	return buf.Bytes()
}

func slotAppLabelExpr(slot *interfaces.Slot) []byte {
	return appLabelExpr(slot.Apps, slot.Snap)
}

func plugAppLabelExpr(plug *interfaces.Plug) []byte {
	return appLabelExpr(plug.Apps, plug.Snap)
}
