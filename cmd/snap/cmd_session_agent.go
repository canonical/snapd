// -*- Mode: Go; indent-tabs-mode: t -*-
// +build linux

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
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/cmd"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/sessionagent"
)

type cmdSessionAgent struct {
	agent sessionagent.SessionAgent
}

var shortSessionAgentHelp = i18n.G("Start the session-agent service")
var longSessionAgentHelp = i18n.G(`
The session-agent command starts the snap user session agent service.
`)

func init() {
	cmd := addCommand("session-agent",
		shortSessionAgentHelp,
		longSessionAgentHelp,
		func() flags.Commander {
			return &cmdSessionAgent{}
		},
		nil,
		nil)
	cmd.hidden = true
}

func (x *cmdSessionAgent) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	x.agent.Version = cmd.Version
	if err := x.agent.Init(); err != nil {
		return err
	}
	x.agent.Start()

	ch := make(chan os.Signal, 3)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM, syscall.SIGUSR1)
	select {
	case sig := <-ch:
		fmt.Fprintf(Stdout, "Exiting on %s.\n", sig)
	case <-x.agent.Dying():
		// something called Stop()
	}

	return x.agent.Stop()
}
