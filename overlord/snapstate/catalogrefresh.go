// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package snapstate

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/snapcore/snapd/advisor"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/randutil"
	"github.com/snapcore/snapd/snapdenv"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/timings"
)

var (
	catalogRefreshDelayBase      = 24 * time.Hour
	catalogRefreshDelayWithDelta = 24*time.Hour + 1 + randutil.RandomDuration(6*time.Hour)
)

type catalogRefresh struct {
	state *state.State

	nextCatalogRefresh time.Time
}

func newCatalogRefresh(st *state.State) *catalogRefresh {
	return &catalogRefresh{state: st}
}

// Ensure will ensure that the catalog refresh happens
func (r *catalogRefresh) Ensure() error {
	r.state.Lock()
	defer r.state.Unlock()

	online, err := isStoreOnline(r.state)
	if err != nil || !online {
		return err
	}

	// sneakily don't do anything if in testing
	if CanAutoRefresh == nil {
		return nil
	}

	// if system is not seeded yet, it is first boot situation
	// do not bother refreshing catalog, snap list is empty anyway
	// beside there is high change device has no internet
	var seeded bool
	err = r.state.Get("seeded", &seeded)
	if errors.Is(err, state.ErrNoState) || !seeded {
		logger.Debugf("CatalogRefresh:Ensure: skipping refresh, system is not seeded yet")
		// not seeded yet
		return nil
	}

	// similar to the not yet seeded case, on uc20 install mode it doesn't make
	// sense to refresh the catalog for an ephemeral system
	deviceCtx, err := DeviceCtx(r.state, nil, nil)
	if err != nil {
		// if we are seeded we should have a device context
		return err
	}

	if deviceCtx.SystemMode() == "install" {
		// skip the refresh
		return nil
	}

	now := time.Now()
	delay := catalogRefreshDelayBase
	if r.nextCatalogRefresh.IsZero() {
		// try to use the timestamp on the sections file
		if st, err := os.Stat(dirs.SnapNamesFile); err == nil && st.ModTime().Before(now) {
			// add the delay with the delta so we spread the load a bit
			r.nextCatalogRefresh = st.ModTime().Add(catalogRefreshDelayWithDelta)
		} else {
			// first time scheduling, add the delta
			delay = catalogRefreshDelayWithDelta
		}
	}

	theStore := Store(r.state, nil)
	needsRefresh := r.nextCatalogRefresh.IsZero() || r.nextCatalogRefresh.Before(now)

	if !needsRefresh {
		return nil
	}

	next := now.Add(delay)
	// catalog refresh does not carry on trying on error
	r.nextCatalogRefresh = next

	logger.Debugf("Catalog refresh starting now; next scheduled for %s.", next)

	err = refreshCatalogs(r.state, theStore)
	switch err {
	case nil:
		logger.Debugf("Catalog refresh succeeded.")
	case advisor.ErrNotSupported:
		// This may happen if Bolt is disabled (e.g. Debian on RISC V).
		logger.Debugf("Catalog refresh is not supported on this system")
		// Do not fail silently so that tests can have the right expectations.
	case store.ErrTooManyRequests:
		logger.Debugf("Catalog refresh postponed.")
		err = nil
	case errSkipCatalogRefreshWhenTesting:
		logger.Debugf("Catalog refresh skipped when testing is enabled")
		err = nil
	default:
		logger.Debugf("Catalog refresh failed: %v.", err)
	}
	return err
}

var newCmdDB = advisor.Create

var errSkipCatalogRefreshWhenTesting = errors.New("skipping when testing is enabled")

func refreshCatalogs(st *state.State, theStore StoreService) error {
	if snapdenv.Testing() && !osutil.GetenvBool("SNAPD_CATALOG_REFRESH") {
		// with snapd testing enabled, SNAPD_CATALOG_REFRESH is gating
		// the catalog refresh
		return errSkipCatalogRefreshWhenTesting
	}

	st.Unlock()
	defer st.Lock()

	perfTimings := timings.New(map[string]string{"ensure": "refresh-catalogs"})

	if err := os.MkdirAll(dirs.SnapCacheDir, 0755); err != nil {
		return fmt.Errorf("cannot create directory %q: %v", dirs.SnapCacheDir, err)
	}

	var sections []string
	var err error
	timings.Run(perfTimings, "get-sections", "query store for sections", func(tm timings.Measurer) {
		sections, err = theStore.Sections(auth.EnsureContextTODO(), nil)
	})
	if err != nil {
		return err
	}
	sort.Strings(sections)
	if err := osutil.AtomicWriteFile(dirs.SnapSectionsFile, []byte(strings.Join(sections, "\n")), 0644, 0); err != nil {
		return err
	}

	namesFile, err := osutil.NewAtomicFile(dirs.SnapNamesFile, 0644, 0, osutil.NoChown, osutil.NoChown)
	if err != nil {
		return err
	}
	defer namesFile.Cancel()

	cmdDB, err := newCmdDB()
	if err != nil {
		return err
	}

	// if all goes well we'll Commit() making this a NOP:
	defer cmdDB.Rollback()

	timings.Run(perfTimings, "write-catalogs", "query store for catalogs", func(tm timings.Measurer) {
		err = theStore.WriteCatalogs(auth.EnsureContextTODO(), namesFile, cmdDB)
	})
	if err != nil {
		return err
	}

	err1 := namesFile.Commit()
	err2 := cmdDB.Commit()

	if err2 != nil {
		return err2
	}

	st.Lock()
	perfTimings.Save(st)
	st.Unlock()

	return err1
}
