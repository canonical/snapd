// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
	"github.com/ddkwork/golibrary/mylog"
	"github.com/jessevdk/go-flags"
	"gopkg.in/yaml.v2"

	"github.com/snapcore/snapd/i18n"
)

var (
	shortCreateCohortHelp = i18n.G("Create cohort keys for a set of snaps")
	longCreateCohortHelp  = i18n.G(`
The create-cohort command creates a set of cohort keys for a given set of snaps.

A cohort is a view or snapshot of a snap's "channel map" at a given point in
time that fixes the set of revisions for the snap given other constraints
(e.g. channel or architecture). The cohort is then identified by an opaque
per-snap key that works across systems. Installations or refreshes of the snap
using a given cohort key would use a fixed revision for up to 90 days, after
which a new set of revisions would be fixed under that same cohort key and a
new 90 days window started.
`)
)

type cmdCreateCohort struct {
	clientMixin
	Positional struct {
		Snaps []anySnapName `positional-arg-name:"<snap>" required:"1"`
	} `positional-args:"yes" required:"yes"`
}

func init() {
	addCommand("create-cohort", shortCreateCohortHelp, longCreateCohortHelp, func() flags.Commander { return &cmdCreateCohort{} }, nil, nil)
}

// output should be YAML, so we use these two as helpers to get that done easy
type cohortInnerYAML struct {
	CohortKey string `yaml:"cohort-key"`
}
type cohortOutYAML struct {
	Cohorts map[string]cohortInnerYAML `yaml:"cohorts"`
}

func (x *cmdCreateCohort) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	snaps := make([]string, len(x.Positional.Snaps))
	for i, s := range x.Positional.Snaps {
		snaps[i] = string(s)
	}

	cohorts := mylog.Check2(x.client.CreateCohorts(snaps))
	if len(cohorts) == 0 || err != nil {
		return err
	}

	var out cohortOutYAML
	out.Cohorts = make(map[string]cohortInnerYAML, len(cohorts))
	for k, v := range cohorts {
		out.Cohorts[k] = cohortInnerYAML{v}
	}

	enc := yaml.NewEncoder(Stdout)
	defer enc.Close()
	return enc.Encode(out)
}
