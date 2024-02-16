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
	"fmt"
	"os"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
)

var (
	shortRemodelHelp = i18n.G("Remodel this device")
	longRemodelHelp  = i18n.G(`
The remodel command changes the model assertion of the device, either to a new
revision or a full new model.

In the process it applies any implied changes to the device: new required
snaps, new kernel or gadget etc.

Snaps and assertions are downloaded from the store unless they are provided as
local files specified by --snap and --assertion options. If using these
options, it is expected that all the needed snaps and assertions are provided
locally, otherwise the remodel will fail.
`)
)

type cmdRemodel struct {
	waitMixin
	SnapFiles      []string `long:"snap"`
	AssertionFiles []string `long:"assertion"`
	Offline        bool     `long:"offline"`
	RemodelOptions struct {
		NewModelFile flags.Filename
	} `positional-args:"true" required:"true"`
}

func init() {
	addCommand("remodel",
		shortRemodelHelp,
		longRemodelHelp,
		func() flags.Commander {
			return &cmdRemodel{}
		},
		waitDescs.also(map[string]string{
			"snap":      i18n.G("Use one or more locally available snaps."),
			"assertion": i18n.G("Use one or more locally available assertion files."),
			"offline":   i18n.G("Use only pre-installed and locally provided snaps and assertions. Providing any snaps or assertions locally implies --offline."),
		}),
		[]argDesc{{
			// TRANSLATORS: This needs to begin with < and end with >
			name: i18n.G("<new model file>"),
			// TRANSLATORS: This should not start with a lowercase letter.
			desc: i18n.G("New model file"),
		}})
}

func (x *cmdRemodel) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}
	newModelFile := x.RemodelOptions.NewModelFile
	modelData, err := os.ReadFile(string(newModelFile))
	if err != nil {
		return err
	}

	var changeID string
	if len(x.SnapFiles) > 0 || len(x.AssertionFiles) > 0 {
		// don't log the request's body as it will be large
		x.client.SetMayLogBody(false)
		changeID, err = x.client.RemodelWithLocalSnaps(modelData, x.SnapFiles, x.AssertionFiles)
		if err != nil {
			return fmt.Errorf("cannot do offline remodel: %v", err)
		}
	} else {
		changeID, err = x.client.Remodel(modelData, client.RemodelOpts{
			Offline: x.Offline,
		})
		if err != nil {
			return fmt.Errorf("cannot remodel: %v", err)
		}
	}

	if _, err := x.wait(changeID); err != nil {
		if err == noWait {
			return nil
		}
		return err
	}
	fmt.Fprintf(Stdout, i18n.G("New model %s set\n"), newModelFile)
	return nil
}
