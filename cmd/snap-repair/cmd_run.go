// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017-2020 Canonical Ltd
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
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snapdenv"
)

func init() {
	const (
		short = "Fetch and run repair assertions as necessary for the device"
		long  = ""
	)
	mylog.Check2(parser.AddCommand("run", short, long, &cmdRun{}))
}

type cmdRun struct{}

var baseURL *url.URL

func init() {
	var baseurl string
	if snapdenv.UseStagingStore() {
		baseurl = "https://api.staging.snapcraft.io/v2/"
	} else {
		baseurl = "https://api.snapcraft.io/v2/"
	}

	// allow redirecting assertion requests under a different base url
	if forcedURL := os.Getenv("SNAPPY_FORCE_SAS_URL"); forcedURL != "" {
		baseurl = forcedURL
	}

	baseURL = mylog.Check2(url.Parse(baseurl))
}

var rootBrandIDs = []string{"canonical"}

func (c *cmdRun) Execute(args []string) error {
	mylog.Check(os.MkdirAll(dirs.SnapRunRepairDir, 0755))

	flock := mylog.Check2(osutil.NewFileLock(filepath.Join(dirs.SnapRunRepairDir, "lock")))
	mylog.Check(flock.TryLock())
	if err == osutil.ErrAlreadyLocked {
		return fmt.Errorf("cannot run, another snap-repair run already executing")
	}

	defer flock.Unlock()

	run := NewRunner()
	run.BaseURL = baseURL
	mylog.Check(run.LoadState())

	for _, rootRepairBrandID := range rootBrandIDs {
		for {
			repair := mylog.Check2(run.Next(rootRepairBrandID))
			if err == ErrRepairNotFound {
				// no more repairs
				break
			}

			// if the store is offline, we want the unit to succeed and not
			// report failures
			if errors.Is(err, errStoreOffline) {
				logger.NoGuardDebugf("running snap repair: %v", err)
				return nil
			}
			mylog.Check(repair.Run())

		}
	}

	return nil
}
