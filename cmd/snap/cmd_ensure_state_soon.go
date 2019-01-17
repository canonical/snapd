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

package main

import (
	"github.com/jessevdk/go-flags"
)

type cmdEnsureStateSoon struct {
	clientMixin
}

func init() {
	cmd := addDebugCommand("ensure-state-soon",
		"(internal) trigger an ensure run in the state engine",
		"(internal) trigger an ensure run in the state engine",
		func() flags.Commander {
			return &cmdEnsureStateSoon{}
		}, nil, nil)
	cmd.hidden = true
}

func (x *cmdEnsureStateSoon) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	return x.client.Debug("ensure-state-soon", nil, nil)
}
