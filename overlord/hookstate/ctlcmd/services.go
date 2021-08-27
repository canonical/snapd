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
	"fmt"
	"sort"
	"text/tabwriter"

	"github.com/snapcore/snapd/client/clientutil"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/overlord/servicestate"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
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

type byApp []*snap.AppInfo

func (a byApp) Len() int      { return len(a) }
func (a byApp) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a byApp) Less(i, j int) bool {
	return a[i].Name < a[j].Name
}

func (c *servicesCommand) Execute([]string) error {
	context, err := c.ensureContext()
	if err != nil {
		return err
	}

	st := context.State()
	svcInfos, err := getServiceInfos(st, context.InstanceName(), c.Positional.ServiceNames)
	if err != nil {
		return err
	}
	sort.Sort(byApp(svcInfos))

	sd := servicestate.NewStatusDecorator(progress.Null)

	services, err := clientutil.ClientAppInfosFromSnapAppInfos(svcInfos, sd)
	if err != nil || len(services) == 0 {
		return err
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
		if svc.DaemonScope == snap.UserDaemon {
			current = "-"
		} else if svc.Active {
			current = i18n.G("active")
		}
		fmt.Fprintf(w, "%s.%s\t%s\t%s\t%s\n", svc.Snap, svc.Name, startup, current, clientutil.ClientAppInfoNotes(&svc))
	}

	return nil
}
