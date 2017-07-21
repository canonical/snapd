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

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
)

type svcStatus struct {
	Positional struct {
		ServiceNames []serviceName `positional-arg-name:"<service>"`
	} `positional-args:"yes"`
}

var (
	shortStatusHelp = i18n.G("Query the status of services")
)

func init() {
	addCommand("status", shortStatusHelp, "", func() flags.Commander { return &svcStatus{} }, nil, nil)
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

	services, err := Client().AppInfos(svcNames(s.Positional.ServiceNames), &client.AppInfoWanted{Services: true})
	if err != nil {
		return err
	}

	w := tabWriter()
	defer w.Flush()

	fmt.Fprintln(w, i18n.G("Snap\tService\tActive\tEnabled"))

	lastSnap := ""
	for _, svc := range services {
		snapMaybe := ""
		if svc.Snap != lastSnap {
			snapMaybe = svc.Snap
			lastSnap = svc.Snap
		}
		fmt.Fprintf(w, "%s\t%s\t%t\t%t\n", snapMaybe, svc.Name, svc.Active, svc.Enabled)
	}

	return nil
}
