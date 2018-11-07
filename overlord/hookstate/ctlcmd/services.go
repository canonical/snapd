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

package ctlcmd

import (
	"errors"
	"fmt"
	"text/tabwriter"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
)

var (
	shortServicesHelp = i18n.G("Query the status of services")
	longServicesHelp  = i18n.G(`
The services command lists information about the services specified.
`)
)

func init() {
	addCommand("services", shortServicesHelp, longServicesHelp, func() command { return &servicesCommand{} })
}

type servicesCommand struct {
	baseCommand
	Positional struct {
		ServiceNames []string `positional-arg-name:"<service>"`
	} `positional-args:"yes"`
}

var errNoContextForServices = errors.New(i18n.G("cannot query services without a context"))

func (c *servicesCommand) Execute([]string) error {
	context := c.context()
	if context == nil {
		return errNoContextForServices
	}

	st := context.State()
	svcInfos, err := getServiceInfos(st, context.InstanceName(), c.Positional.ServiceNames)
	if err != nil {
		return err
	}

	services := client.AppInfosFromSnapAppInfos(svcInfos)
	if len(services) == 0 {
		return nil
	}

	w := tabwriter.NewWriter(c.stdout, 5, 3, 2, ' ', 0)
	defer w.Flush()

	fmt.Fprintln(w, i18n.G("Service\tStartup\tCurrent\tNotes"))

	for _, svc := range services {
		startup := i18n.G("disabled")
		if svc.Enabled {
			startup = i18n.G("enabled")
		}
		current := i18n.G("inactive")
		if svc.Active {
			current = i18n.G("active")
		}
		fmt.Fprintf(w, "%s.%s\t%s\t%s\t%s\n", svc.Snap, svc.Name, startup, current, svc.Notes())
	}

	return nil
}
