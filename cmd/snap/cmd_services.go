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
		ServiceNames []serviceName
	} `positional-args:"yes"`
}

type svcLogs struct {
	N          string `short:"n" default:"10"`
	Follow     bool   `short:"f"`
	Positional struct {
		ServiceNames []serviceName `required:"1"`
	} `positional-args:"yes" required:"yes"`
}

var (
	shortServicesHelp = i18n.G("Query the status of services")
	longServicesHelp  = i18n.G(`
The services command lists information about the services specified, or about
the services in all currently installed snaps.
`)
	shortLogsHelp = i18n.G("Retrieve logs of services")
	longLogsHelp  = i18n.G(`
The logs command fetches logs of the given services and displays them in
chronological order.
`)
	shortStartHelp = i18n.G("Start services")
	longStartHelp  = i18n.G(`
The start command starts, and optionally enables, the given services.
`)
	shortStopHelp = i18n.G("Stop services")
	longStopHelp  = i18n.G(`
The stop command stops, and optionally disables, the given services.
`)
	shortRestartHelp = i18n.G("Restart services")
	longRestartHelp  = i18n.G(`
The restart command restarts the given services.

If the --reload option is given, for each service whose app has a reload
command, a reload is performed instead of a restart.
`)
)

func init() {
	argdescs := []argDesc{{
		// TRANSLATORS: This needs to be wrapped in <>s.
		name: i18n.G("<service>"),
		desc: i18n.G("A service specification, which can be just a snap name (for all services in the snap), or <snap>.<app> for a single service."),
	}}
	addCommand("services", shortServicesHelp, longServicesHelp, func() flags.Commander { return &svcStatus{} }, nil, argdescs)
	addCommand("logs", shortLogsHelp, longLogsHelp, func() flags.Commander { return &svcLogs{} },
		map[string]string{
			"n": i18n.G("Show only the given number of lines, or 'all'."),
			"f": i18n.G("Wait for new lines and print them as they come in."),
		}, argdescs)

	addCommand("start", shortStartHelp, longStartHelp, func() flags.Commander { return &svcStart{} },
		waitDescs.also(map[string]string{
			"enable": i18n.G("As well as starting the service now, arrange for it to be started on boot."),
		}), argdescs)
	addCommand("stop", shortStopHelp, longStopHelp, func() flags.Commander { return &svcStop{} },
		waitDescs.also(map[string]string{
			"disable": i18n.G("As well as stopping the service now, arrange for it to no longer be started on boot."),
		}), argdescs)
	addCommand("restart", shortRestartHelp, longRestartHelp, func() flags.Commander { return &svcRestart{} },
		waitDescs.also(map[string]string{
			"reload": i18n.G("If the service has a reload command, use it instead of restarting."),
		}), argdescs)
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
		ServiceNames []serviceName `required:"1"`
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
		ServiceNames []serviceName `required:"1"`
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
		ServiceNames []serviceName `required:"1"`
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
