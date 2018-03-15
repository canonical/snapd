// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package release

import (
	"io/ioutil"
	"sort"
	"strings"
)

var (
	secCompAvailableActionsPath = "/proc/sys/kernel/seccomp/actions_avail"
)

var SecCompActions []string

func init() {
	SecCompActions = getSecCompActions()
}

func MockSecCompActions(actions []string) (restore func()) {
	old := SecCompActions
	SecCompActions = actions
	return func() { SecCompActions = old }
}

// SecCompActions returns a sorted list of seccomp actions like
// []string{"allow", "errno", "kill", "log", "trace", "trap"}.
func getSecCompActions() []string {
	var actions []string
	contents, err := ioutil.ReadFile(secCompAvailableActionsPath)
	if err != nil {
		return actions
	}

	seccompActions := strings.Split(strings.TrimRight(string(contents), "\n"), " ")
	sort.Strings(seccompActions)

	return seccompActions
}

func SecCompSupportsAction(action string) bool {
	i := sort.SearchStrings(SecCompActions, action)
	if i < len(SecCompActions) && SecCompActions[i] == action {
		return true
	}
	return false
}
