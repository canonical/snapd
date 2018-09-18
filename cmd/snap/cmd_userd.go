// -*- Mode: Go; indent-tabs-mode: t -*-

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

package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/userd"
)

type cmdUserd struct {
	userd userd.Userd

	Autostart bool `long:"autostart"`
}

var shortUserdHelp = i18n.G("Start the userd service")
var longUserdHelp = i18n.G(`
The userd command starts the snap user session service.
`)

func init() {
	cmd := addCommand("userd",
		shortUserdHelp,
		longUserdHelp,
		func() flags.Commander {
			return &cmdUserd{}
		}, map[string]string{
			"autostart": i18n.G("Autostart user applications"),
		}, nil)
	cmd.hidden = true
}

func (x *cmdUserd) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	if x.Autostart {
		return x.runAutostart()
	}

	if err := x.userd.Init(); err != nil {
		return err
	}
	x.userd.Start()

	ch := make(chan os.Signal)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM, syscall.SIGUSR1)
	select {
	case sig := <-ch:
		fmt.Fprintf(Stdout, "Exiting on %s.\n", sig)
	case <-x.userd.Dying():
		// something called Stop()
	}

	return x.userd.Stop()
}

func (x *cmdUserd) runAutostart() error {
	if err := userd.AutostartSessionApps(); err != nil {
		return fmt.Errorf("autostart failed for the following apps:\n%v", err)
	}
	return nil
}
