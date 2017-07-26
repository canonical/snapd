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

type svcServices struct {
	Positional struct {
		ServiceNames []serviceName `positional-arg-name:"<service>"`
	} `positional-args:"yes"`
}

var (
	shortServicesHelp = i18n.G("Query the status of services")
)

func init() {
	addCommand("services", shortServicesHelp, "", func() flags.Commander { return &svcServices{} }, nil, nil)
}

func svcNames(s []serviceName) []string {
	svcNames := make([]string, len(s))
	for i, svcName := range s {
		svcNames[i] = string(svcName)
	}
	return svcNames
}

func (s *svcServices) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	services, err := Client().Apps(svcNames(s.Positional.ServiceNames), client.AppOptions{Service: true})
	if err != nil {
		return err
	}

	w := tabWriter()
	defer w.Flush()

	fmt.Fprintln(w, i18n.G("Snap\tService\tActive\tEnabled"))

	for _, svc := range services {
		fmt.Fprintf(w, "%s\t%s\t%t\t%t\n", svc.Snap, svc.Name, svc.Active, svc.Enabled)
	}

	return nil
}
