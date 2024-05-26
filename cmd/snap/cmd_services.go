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
	"os/user"
	"strconv"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/client/clientutil"
	"github.com/snapcore/snapd/i18n"
)

type svcStatus struct {
	clientMixin
	Positional struct {
		ServiceNames []serviceName
	} `positional-args:"yes"`
	Global bool `long:"global" short:"g"`
	User   bool `long:"user" short:"u"`
}

type svcLogs struct {
	clientMixin
	timeMixin
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

If executed as root user, the 'Startup' column of any user service will be whether
it's globally enabled (i.e systemctl is-enabled). To view the actual 'Startup'|'Current'
status of the user services for the root user itself, --user can be provided.

If executed as a non-root user, the 'Startup'|'Current' status of user services 
will be the current status for the invoking user. To view the global enablement
status of user services, --global can be provided.
`)
	shortLogsHelp = i18n.G("Retrieve logs for services")
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
		// TRANSLATORS: This needs to begin with < and end with >
		name: i18n.G("<service>"),
		// TRANSLATORS: This should not start with a lowercase letter.
		desc: i18n.G("A service specification, which can be just a snap name (for all services in the snap), or <snap>.<app> for a single service."),
	}}
	addCommand("services", shortServicesHelp, longServicesHelp, func() flags.Commander { return &svcStatus{} }, map[string]string{
		// TRANSLATORS: This should not start with a lowercase letter.
		"global": i18n.G("Show the global enable status for user services instead of the status for the current user."),
		// TRANSLATORS: This should not start with a lowercase letter.
		"user": i18n.G("Show the current status of the user services instead of the global enable status."),
	}, argdescs)
	addCommand("logs", shortLogsHelp, longLogsHelp, func() flags.Commander { return &svcLogs{} },
		timeDescs.also(map[string]string{
			// TRANSLATORS: This should not start with a lowercase letter.
			"n": i18n.G("Show only the given number of lines, or 'all'."),
			// TRANSLATORS: This should not start with a lowercase letter.
			"f": i18n.G("Wait for new lines and print them as they come in."),
		}), argdescs)

	addCommand("start", shortStartHelp, longStartHelp, func() flags.Commander { return &svcStart{} },
		waitDescs.also(userAndScopeDescs).also(map[string]string{
			// TRANSLATORS: This should not start with a lowercase letter.
			"enable": i18n.G("As well as starting the service now, arrange for it to be started on boot."),
		}), argdescs)
	addCommand("stop", shortStopHelp, longStopHelp, func() flags.Commander { return &svcStop{} },
		waitDescs.also(userAndScopeDescs).also(map[string]string{
			// TRANSLATORS: This should not start with a lowercase letter.
			"disable": i18n.G("As well as stopping the service now, arrange for it to no longer be started on boot."),
		}), argdescs)
	addCommand("restart", shortRestartHelp, longRestartHelp, func() flags.Commander { return &svcRestart{} },
		waitDescs.also(userAndScopeDescs).also(map[string]string{
			// TRANSLATORS: This should not start with a lowercase letter.
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

func (s *svcStatus) showGlobalEnablement(u *user.User) bool {
	if u.Uid == "0" && !s.User {
		return true
	} else if u.Uid != "0" && s.Global {
		return true
	}
	return false
}

func (s *svcStatus) validateArguments() error {
	// can't use --global and --user together
	if s.Global && s.User {
		return fmt.Errorf(i18n.G("cannot combine --global and --user switches."))
	}
	return nil
}

func (s *svcStatus) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}
	mylog.Check(s.validateArguments())

	u := mylog.Check2(userCurrent())

	isGlobal := s.showGlobalEnablement(u)
	services := mylog.Check2(s.client.Apps(svcNames(s.Positional.ServiceNames), client.AppOptions{
		Service: true,
		Global:  isGlobal,
	}))

	if len(services) == 0 {
		fmt.Fprintln(Stderr, i18n.G("There are no services provided by installed snaps."))
		return nil
	}

	w := tabWriter()
	defer w.Flush()

	fmt.Fprintln(w, i18n.G("Service\tStartup\tCurrent\tNotes"))
	for _, svc := range services {
		fmt.Fprintln(w, clientutil.FmtServiceStatus(svc, isGlobal))
	}
	return nil
}

func (s *svcLogs) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	sN := -1
	if s.N != "all" {
		n := mylog.Check2(strconv.ParseInt(s.N, 0, 32))
		if n < 0 || err != nil {
			return fmt.Errorf(i18n.G("invalid argument for flag ‘-n’: expected a non-negative integer argument, or “all”."))
		}
		sN = int(n)
	}

	logs := mylog.Check2(s.client.Logs(svcNames(s.Positional.ServiceNames), client.LogOptions{N: sN, Follow: s.Follow}))

	for log := range logs {
		if s.AbsTime {
			fmt.Fprintln(Stdout, log.StringInUTC())
		} else {
			fmt.Fprintln(Stdout, log)
		}
	}

	return nil
}

var userAndScopeDescs = mixinDescs{
	// TRANSLATORS: This should not start with a lowercase letter.
	"system": i18n.G("The operation should only affect system services."),
	// TRANSLATORS: This should not start with a lowercase letter.
	"user": i18n.G("The operation should only affect user services for the current user."),
	// TRANSLATORS: This should not start with a lowercase letter.
	"users": i18n.G("If provided and set to 'all', the operation should affect services for all users."),
}

type svcStart struct {
	waitMixin
	clientutil.ServiceScopeOptions
	Positional struct {
		ServiceNames []serviceName `required:"1"`
	} `positional-args:"yes" required:"yes"`
	Enable bool `long:"enable"`
}

func (s *svcStart) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}
	mylog.Check(s.Validate())

	names := svcNames(s.Positional.ServiceNames)
	changeID := mylog.Check2(s.client.Start(names, s.Scope(), s.Users(), client.StartOptions{Enable: s.Enable}))
	mylog.Check2(s.wait(changeID))

	fmt.Fprintln(Stdout, i18n.G("Started."))

	return nil
}

type svcStop struct {
	waitMixin
	clientutil.ServiceScopeOptions
	Positional struct {
		ServiceNames []serviceName `required:"1"`
	} `positional-args:"yes" required:"yes"`
	Disable bool `long:"disable"`
}

func (s *svcStop) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}
	mylog.Check(s.Validate())

	names := svcNames(s.Positional.ServiceNames)
	changeID := mylog.Check2(s.client.Stop(names, s.Scope(), s.Users(), client.StopOptions{Disable: s.Disable}))
	mylog.Check2(s.wait(changeID))

	fmt.Fprintln(Stdout, i18n.G("Stopped."))

	return nil
}

type svcRestart struct {
	waitMixin
	clientutil.ServiceScopeOptions
	Positional struct {
		ServiceNames []serviceName `required:"1"`
	} `positional-args:"yes" required:"yes"`
	Reload bool `long:"reload"`
}

func (s *svcRestart) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}
	mylog.Check(s.Validate())

	names := svcNames(s.Positional.ServiceNames)
	changeID := mylog.Check2(s.client.Restart(names, s.Scope(), s.Users(), client.RestartOptions{Reload: s.Reload}))
	mylog.Check2(s.wait(changeID))

	fmt.Fprintln(Stdout, i18n.G("Restarted."))

	return nil
}
