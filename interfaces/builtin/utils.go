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
	"fmt"
	"sort"
	"strings"

	"github.com/ubuntu-core/snappy/interfaces"
)

// slotAppLabelExpr returns the specification of the apparmor label describing
// all the apps bound to a given slot. The result has one of three forms,
// depending on how apps are bound to the slot:
//
// - "snap.$snap.$app" if there is exactly one app bound
// - "snap.$snap.{$app1,...$appN}" if there are some, but not all, apps bound
// - "snap.$snap.*" if all apps are bound to the slot
func slotAppLabelExpr(slot *interfaces.Slot) []byte {
	snapName := slot.Snap.Name()
	if len(slot.Apps) == 1 {
		for appName := range slot.Apps {
			return []byte(fmt.Sprintf("snap.%s.%s", snapName, appName))
		}
	} else if len(slot.Apps) == len(slot.Snap.Apps) {
		return []byte(fmt.Sprintf("snap.%s.*", snapName))
	}
	appNames := make([]string, 0, len(slot.Apps))
	for appName := range slot.Apps {
		appNames = append(appNames, appName)
	}
	sort.Strings(appNames)
	return []byte(fmt.Sprintf("snap.%s.{%s}", snapName, strings.Join(appNames, ",")))
}
