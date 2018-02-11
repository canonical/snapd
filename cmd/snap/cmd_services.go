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
	N          string `short:"n" default:"10"`
	Follow     bool   `short:"f"`
	Positional struct {
		ServiceNames []serviceName `positional-arg-name:"<service>" required:"1"`
	} `positional-args:"yes" required:"yes"`
}

var (
	shortServicesHelp = i18n.G("Query the status of services")
	shortLogsHelp     = i18n.G("Retrieve logs of services")
	shortStartHelp    = i18n.G("Start services")
	shortStopHelp     = i18n.G("Stop services")
	shortRestartHelp  = i18n.G("Restart services")
)

func init() {
	addCommand("services", shortServicesHelp, "", func() flags.Commander { return &svcStatus{} }, nil, nil)
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

	services, err := Client().Apps(svcNames(s.Positional.ServiceNames), client.AppOptions{Service: true})
	if err != nil {
		return err
	}

	w := tabWriter()
	defer w.Flush()

	fmt.Fprintln(w, i18n.G("Service\tStartup\tCurrent"))

	for _, svc := range services {
		startup := i18n.G("disabled")
		if svc.Enabled {
			startup = i18n.G("enabled")
		}
		current := i18n.G("inactive")
		if svc.Active {
			current = i18n.G("active")
		}
		fmt.Fprintf(w, "%s.%s\t%s\t%s\n", svc.Snap, svc.Name, startup, current)
	}

	return nil
}

func (s *svcLogs) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	sN := -1
	if s.N != "all" {
		n, err := strconv.ParseInt(s.N, 0, 32)
		if n < 0 || err != nil {
			return fmt.Errorf(i18n.G("invalid argument for flag ‘-n’: expected a non-negative integer argument, or “all”."))
		}
		sN = int(n)
	}

	logs, err := Client().Logs(svcNames(s.Positional.ServiceNames), client.LogOptions{N: sN, Follow: s.Follow})
	if err != nil {
		return err
	}

	for log := range logs {
		fmt.Fprintln(Stdout, log)
	}

	return nil
}

type svcStart struct {
	waitMixin
	Positional struct {
		ServiceNames []serviceName `positional-arg-name:"<service>" required:"1"`
	} `positional-args:"yes" required:"yes"`
	Enable bool `long:"enable"`
}

func (s *svcStart) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}
	cli := Client()
	names := svcNames(s.Positional.ServiceNames)
	changeID, err := cli.Start(names, client.StartOptions{Enable: s.Enable})
	if err != nil {
		return err
	}
	if _, err := s.wait(cli, changeID); err != nil {
		if err == noWait {
			return nil
		}
		return err
	}

	fmt.Fprintf(Stdout, i18n.G("Started.\n"))

	return nil
}

type svcStop struct {
	waitMixin
	Positional struct {
		ServiceNames []serviceName `positional-arg-name:"<service>" required:"1"`
	} `positional-args:"yes" required:"yes"`
	Disable bool `long:"disable"`
}

func (s *svcStop) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}
	cli := Client()
	names := svcNames(s.Positional.ServiceNames)
	changeID, err := cli.Stop(names, client.StopOptions{Disable: s.Disable})
	if err != nil {
		return err
	}
	if _, err := s.wait(cli, changeID); err != nil {
		if err == noWait {
			return nil
		}
		return err
	}

	fmt.Fprintf(Stdout, i18n.G("Stopped.\n"))

	return nil
}

type svcRestart struct {
	waitMixin
	Positional struct {
		ServiceNames []serviceName `positional-arg-name:"<service>" required:"1"`
	} `positional-args:"yes" required:"yes"`
	Reload bool `long:"reload"`
}

func (s *svcRestart) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}
	cli := Client()
	names := svcNames(s.Positional.ServiceNames)
	changeID, err := cli.Restart(names, client.RestartOptions{Reload: s.Reload})
	if err != nil {
		return err
	}
	if _, err := s.wait(cli, changeID); err != nil {
		if err == noWait {
			return nil
		}
		return err
	}

	fmt.Fprintf(Stdout, i18n.G("Restarted.\n"))

	return nil
}
