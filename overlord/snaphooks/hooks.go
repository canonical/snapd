/*
 * Copyright (C) 2017 Canonical Ltd
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

package snaphooks

import (
	"fmt"
	"regexp"

	"github.com/snapcore/snapd/i18n/dumb"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
)

func init() {
	snapstate.InstallHookSetup = InstallHookSetup
	snapstate.RemoveHookSetup = RemoveHookSetup
}

func InstallHookSetup(st *state.State, snapName string) *state.Task {
	hooksup := &hookstate.HookSetup{
		Snap:     snapName,
		Hook:     "install",
		Optional: true,
	}

	summary := fmt.Sprintf(i18n.G("Install hook of snap %q"), hooksup.Snap)
	task := hookstate.HookTask(st, summary, hooksup, nil)

	return task
}

type snapHookHandler struct {
}

func (h *snapHookHandler) Before() error {
	return nil
}

func (h *snapHookHandler) Done() error {
	return nil
}

func (h *snapHookHandler) Error(err error) error {
	return nil
}

func RemoveHookSetup(st *state.State, snapName string) *state.Task {
	hooksup := &hookstate.HookSetup{
		Snap:        snapName,
		Hook:        "remove",
		Optional:    true,
		IgnoreError: true,
	}

	summary := fmt.Sprintf(i18n.G("Remove hook of snap %q"), hooksup.Snap)
	task := hookstate.HookTask(st, summary, hooksup, nil)

	return task
}

func SetupHooks(hookMgr *hookstate.HookManager) {
	handlerGenerator := func(context *hookstate.Context) hookstate.Handler {
		return &snapHookHandler{}
	}

	hookMgr.Register(regexp.MustCompile("^install$"), handlerGenerator)
	hookMgr.Register(regexp.MustCompile("^remove$"), handlerGenerator)
}
