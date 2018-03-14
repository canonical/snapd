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

package settings

import (
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/state"
)

// ProblemReportsDisabled returns true if the problem reports are disabled
// via the "core.problem-reports.disabled" settings.
//
// The state must be locked when this is called.
func ProblemReportsDisabled(st *state.State) bool {
	var disableProblemReports bool

	tr := config.NewTransaction(st)
	if err := tr.GetMaybe("core", "problem-reports.disabled", &disableProblemReports); err != nil {
		logger.Noticef("cannot get problem report setting: %v", err)
	}

	return disableProblemReports
}
