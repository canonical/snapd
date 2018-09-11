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

package patch

import (
	"fmt"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/overlord/state"
)

func init() {
	patches[3] = []PatchFunc{patch3}
}

// patch3:
// - migrates pending tasks and add {start,stop}-snap-services tasks
func patch3(s *state.State) error {

	// migrate all pending tasks and insert "{start,stop}-snap-server"
	for _, t := range s.Tasks() {
		if t.Status().Ready() {
			continue
		}

		if t.Kind() == "link-snap" {
			startSnapServices := s.NewTask("start-snap-services", fmt.Sprintf(i18n.G("Start snap services")))
			startSnapServices.Set("snap-setup-task", t.ID())
			startSnapServices.WaitFor(t)

			chg := t.Change()
			chg.AddTask(startSnapServices)
		}

		if t.Kind() == "unlink-snap" || t.Kind() == "unlink-current-snap" {
			stopSnapServices := s.NewTask("stop-snap-services", fmt.Sprintf(i18n.G("Stop snap services")))
			stopSnapServices.Set("snap-setup-task", t.ID())
			t.WaitFor(stopSnapServices)

			chg := t.Change()
			chg.AddTask(stopSnapServices)
		}
	}

	return nil
}
