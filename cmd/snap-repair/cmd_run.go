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

	if _, err := parser.AddCommand("run", short, long, &cmdRun{}); err != nil {
		panic(err)
	}

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

	var err error
	baseURL, err = url.Parse(baseurl)
	if err != nil {
		panic(fmt.Sprintf("cannot setup base url: %v", err))
	}
}

var rootBrandIDs = []string{"canonical"}

func (c *cmdRun) Execute(args []string) error {
	if err := os.MkdirAll(dirs.SnapRunRepairDir, 0755); err != nil {
		return err
	}
	flock, err := osutil.NewFileLock(filepath.Join(dirs.SnapRunRepairDir, "lock"))
	if err != nil {
		return err
	}
	err = flock.TryLock()
	if err == osutil.ErrAlreadyLocked {
		return fmt.Errorf("cannot run, another snap-repair run already executing")
	}
	if err != nil {
		return err
	}
	defer flock.Unlock()

	run := NewRunner()
	run.BaseURL = baseURL
	err = run.LoadState()
	if err != nil {
		return err
	}

	for _, rootRepairBrandID := range rootBrandIDs {
		for {
			repair, err := run.Next(rootRepairBrandID)
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

			if err != nil {
				return err
			}

			if err := repair.Run(); err != nil {
				return err
			}
		}
	}

	return nil
}
