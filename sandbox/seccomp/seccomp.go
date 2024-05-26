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

package seccomp

import (
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/ddkwork/golibrary/mylog"
)

var secCompProber = &secCompProbe{}

// Actions returns a sorted list of seccomp actions like
// []string{"allow", "errno", "kill", "log", "trace", "trap"}.
func Actions() []string {
	return secCompProber.actions()
}

func SupportsAction(action string) bool {
	actions := Actions()
	i := sort.SearchStrings(actions, action)
	if i < len(actions) && actions[i] == action {
		return true
	}
	return false
}

// probing

type secCompProbe struct {
	probedActions []string

	once sync.Once
}

func (scp *secCompProbe) actions() []string {
	scp.once.Do(func() {
		scp.probedActions = probeActions()
	})
	return scp.probedActions
}

var osReadFile = os.ReadFile

func probeActions() []string {
	contents := mylog.Check2(osReadFile("/proc/sys/kernel/seccomp/actions_avail"))

	actions := strings.Split(strings.TrimRight(string(contents), "\n"), " ")
	sort.Strings(actions)
	return actions
}

// mocking

func MockActions(actions []string) (restore func()) {
	old := secCompProber
	secCompProber = &secCompProbe{
		probedActions: actions,
	}
	secCompProber.once.Do(func() {})
	return func() {
		secCompProber = old
	}
}
