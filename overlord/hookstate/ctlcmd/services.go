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
	"context"
	"fmt"
	"sort"
	"text/tabwriter"

	"github.com/ddkwork/golibrary/mylog"
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
	Global bool `long:"global" short:"g" description:"Show the global enable status for user services instead of the status for the current user"`
	User   bool `long:"user" short:"u" description:"Show the current status of the user services instead of the global enable status"`
}

type byApp []*snap.AppInfo

func (a byApp) Len() int      { return len(a) }
func (a byApp) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a byApp) Less(i, j int) bool {
	return a[i].Name < a[j].Name
}

var newStatusDecorator = func(ctx context.Context, isGlobal bool, uid string) clientutil.StatusDecorator {
	if isGlobal {
		return servicestate.NewStatusDecorator(progress.Null)
	} else {
		return servicestate.NewStatusDecoratorForUid(progress.Null, ctx, uid)
	}
}

func (c *servicesCommand) showGlobalEnablement() bool {
	if c.uid == "0" && !c.User {
		return true
	} else if c.uid != "0" && c.Global {
		return true
	}
	return false
}

func (c *servicesCommand) validateArguments() error {
	// can't use --global and --user together
	if c.Global && c.User {
		return fmt.Errorf(i18n.G("cannot combine --global and --user switches."))
	}
	return nil
}

// The 'snapctl services' command is one of the few commands that can run as
// non-root through snapctl.
func (c *servicesCommand) Execute([]string) error {
	ctx := mylog.Check2(c.ensureContext())
	mylog.Check(c.validateArguments())

	st := ctx.State()
	svcInfos := mylog.Check2(getServiceInfos(st, ctx.InstanceName(), c.Positional.ServiceNames))

	sort.Sort(byApp(svcInfos))

	isGlobal := c.showGlobalEnablement()
	sd := newStatusDecorator(context.TODO(), isGlobal, c.uid)
	services := mylog.Check2(clientutil.ClientAppInfosFromSnapAppInfos(svcInfos, sd))
	if err != nil || len(services) == 0 {
		return err
	}

	w := tabwriter.NewWriter(c.stdout, 5, 3, 2, ' ', 0)
	defer w.Flush()

	fmt.Fprintln(w, i18n.G("Service\tStartup\tCurrent\tNotes"))
	for _, svc := range services {
		fmt.Fprintln(w, clientutil.FmtServiceStatus(&svc, isGlobal))
	}

	return nil
}
