// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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
	"strconv"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
)

type svcStatus struct {
	Positional struct {
		ServiceNames []serviceName `positional-arg-name:"<service>"`
	} `positional-args:"yes"`
}

type svcLogs struct {
	N          string `short:"n" default:"all"`
	Follow     bool   `short:"f"`
	Positional struct {
		ServiceNames []serviceName `positional-arg-name:"<service>" required:"1"`
	} `positional-args:"yes" required:"yes"`
}

var (
	shortStatusHelp  = i18n.G("Query the status of services")
	shortLogsHelp    = i18n.G("Query the logs of a service")
	shortStartHelp   = i18n.G("Start services")
	shortStopHelp    = i18n.G("Stop services")
	shortRestartHelp = i18n.G("Restart services")
)

func init() {
	addCommand("status", shortStatusHelp, "", func() flags.Commander { return &svcStatus{} }, nil, nil)
	addCommand("logs", shortLogsHelp, "", func() flags.Commander { return &svcLogs{} }, nil, nil)

	addCommand("start", shortStartHelp, "", func() flags.Commander { return &svcStart{} }, nil, nil)
	addCommand("stop", shortStopHelp, "", func() flags.Commander { return &svcStop{} }, nil, nil)
	addCommand("restart", shortRestartHelp, "", func() flags.Commander { return &svcRestart{} }, nil, nil)
}

func svcNames(s []serviceName) []string {
	svcNames := make([]string, len(s))
	for i, svcName := range s {
		svcNames[i] = string(svcName)
	}
	return svcNames
}

func (s *svcStatus) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	services, err := Client().ServiceStatus(svcNames(s.Positional.ServiceNames))
	if err != nil {
		return err
	}

	w := tabWriter()
	defer w.Flush()

	fmt.Fprintln(w, i18n.G("Snap\tService\tState"))

	lastSnap := ""
	for _, svc := range services {
		snapMaybe := ""
		if svc.Snap != lastSnap {
			snapMaybe = svc.Snap
			lastSnap = svc.Snap
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", snapMaybe, svc.Name, svc.StatusString())
	}

	return nil
}

func (s *svcLogs) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	if s.N != "all" {
		n, err := strconv.ParseInt(s.N, 0, 32)
		if n < 0 || err != nil {
			return fmt.Errorf(i18n.G("invalid argument for flag ‘-n’: expected a non-negative integer argument, or “all”."))
		}
	}

	logs, err := Client().ServiceLogs(svcNames(s.Positional.ServiceNames), s.N, s.Follow)
	if err != nil {
		return err
	}

	for log := range logs {
		fmt.Fprintln(Stdout, log)
	}

	return nil
}

type svcChangeMixin struct {
	waitMixin
	Positional struct {
		ServiceNames []serviceName `positional-arg-name:"<service>" required:"1"`
	} `positional-args:"yes" required:"yes"`
}

func (s svcChangeMixin) execute(args []string, action string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}
	cli := Client()
	names := svcNames(s.Positional.ServiceNames)
	op := &client.ServiceOp{Action: action, Services: names}
	changeID, err := cli.RunServiceOp(op)
	if err != nil {
		return err
	}
	if _, err := s.wait(cli, changeID); err != nil {
		if err == noWait {
			return nil
		}
		return err
	}

	fmt.Fprintf(Stdout, "%s: Done.\n", op.Description())

	return nil
}

type svcStart struct {
	Enable bool `long:"enable"`
	svcChangeMixin
}

func (s *svcStart) Execute(args []string) error {
	action := "start"
	if s.Enable {
		action = "enable-now"
	}
	return s.execute(args, action)
}

type svcStop struct {
	Disable bool `long:"disable"`
	svcChangeMixin
}

func (s *svcStop) Execute(args []string) error {
	action := "stop"
	if s.Disable {
		action = "disable-now"
	}
	return s.execute(args, action)
}

type svcRestart struct {
	Reload bool `long:"reload"`
	svcChangeMixin
}

func (s *svcRestart) Execute(args []string) error {
	action := "restart"
	if s.Reload {
		action = "try-reload-or-restart"
	}
	return s.execute(args, action)
}
