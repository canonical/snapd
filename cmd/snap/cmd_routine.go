// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package main

import (
	"github.com/snapcore/snapd/i18n"
)

type cmdRoutine struct{}

var (
	shortRoutineHelp = i18n.G("Run routine commands")
	longRoutineHelp  = i18n.G(`
The routine command contains a selection of additional sub-commands.

Routine commands are not intended to be directly invoked by the user.
Instead, they are intended to be called by other programs and produce
machine readable output.
`)
)
