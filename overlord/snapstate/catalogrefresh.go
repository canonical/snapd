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
	"context"
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
	"github.com/snapcore/snapd/overlord/swfeats"
	"github.com/snapcore/snapd/randutil"
	"github.com/snapcore/snapd/snapdenv"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/timings"
)

var (
	catalogRefreshDelayBase = 24 * time.Hour
)

func init() {
	swfeats.RegisterEnsure("SnapManager", "catalogRefresh.EnsureAfterSeed")
}

type catalogRefresh struct {
	state *state.State

	nextCatalogRefresh           time.Time
	catalogRefreshDelayWithDelta time.Duration

	// ctx parent context for store requests
	ctx context.Context
	// cancel cancels ctx and any in-flight store request
	cancel context.CancelFunc

	catalogC chan struct{}
}

func newCatalogRefresh(st *state.State) *catalogRefresh {
	// Derive from EnsureContextTODO so IsEnsureContext() still returns true.
	ctx, cancel := context.WithCancel(auth.EnsureContextTODO())
	return &catalogRefresh{state: st, ctx: ctx, cancel: cancel}
}

func (r *catalogRefresh) ShutDown() {
	r.cancel()
}

// Stop cancels any in-flight refresh and waits for it to finish. It must be
// called without holding the state lock.
func (r *catalogRefresh) Stop() {
	r.cancel()
	if r.catalogC != nil {
		<-r.catalogC
	}
}

// EnsureAfterSeed will ensure that the catalog refresh happens after seeding.
func (r *catalogRefresh) EnsureAfterSeed(deviceCtx DeviceContext) error {
	r.state.Lock()
	defer r.state.Unlock()

	if r.catalogRefreshDelayWithDelta == 0 {
		r.catalogRefreshDelayWithDelta = catalogRefreshDelayBase + 1 + randutil.RandomDuration(6*time.Hour)
	}

	online, err := isStoreOnline(r.state)
	if err != nil || !online {
		return err
	}

	// sneakily don't do anything if in testing
	if CanAutoRefresh == nil {
		return nil
	}

	// similar to the not yet seeded case, on uc20 install mode it doesn't make
	// sense to refresh the catalog for an ephemeral system
	if deviceCtx.SystemMode() == "install" {
		// skip the refresh
		return nil
	}

	logger.Trace("ensure", "manager", "SnapManager", "func", "catalogRefresh.EnsureAfterSeed")

	now := time.Now()
	delay := catalogRefreshDelayBase
	if r.nextCatalogRefresh.IsZero() {
		// try to use the timestamp on the sections file
		if st, err := os.Stat(dirs.SnapNamesFile); err == nil && st.ModTime().Before(now) {
			// add the delay with the delta so we spread the load a bit
			r.nextCatalogRefresh = st.ModTime().Add(r.catalogRefreshDelayWithDelta)
		} else {
			// first time scheduling, add the delta
			delay = r.catalogRefreshDelayWithDelta
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

	if r.catalogC != nil {
		// we already have a refresh running
		select {
		case <-r.catalogC:
			// channel got closed, previous refresh is done
			r.catalogC = nil
		default:
			logger.Debugf("Previous catalog refresh still running; next scheduled for %s.", next)
			return nil
		}
	}

	logger.Debugf("Catalog refresh starting now; next scheduled for %s.", next)

	r.catalogC = make(chan struct{})

	go func() {
		defer close(r.catalogC)
		timings, err := refreshCatalogs(r.ctx, theStore)
		if err != nil {
			switch {
			case errors.Is(err, store.ErrTooManyRequests):
				logger.Debug("Catalog refresh postponed.")
			case errors.Is(err, errSkipCatalogRefreshWhenTesting):
				logger.Debug("Catalog refresh skipped when testing is enabled")
			case errors.Is(err, context.Canceled):
				// Canceled catalog refresh is not treated as an error.
				logger.Debug("Catalog refresh canceled.")
			default:
				logger.Noticef("Catalog refresh failed: %v.", err)
			}
			return
		}
		logger.Debug("Catalog refresh succeeded.")
		r.state.Lock()
		defer r.state.Unlock()
		// save the timings, since we're holding the lock anyway
		timings.Save(r.state)
	}()
	return nil
}

var newCmdDB = advisor.Create

var errSkipCatalogRefreshWhenTesting = errors.New("skipping when testing is enabled")

func refreshCatalogs(ctx context.Context, theStore StoreService) (*timings.Timings, error) {
	if snapdenv.Testing() && !osutil.GetenvBool("SNAPD_CATALOG_REFRESH") {
		// with snapd testing enabled, SNAPD_CATALOG_REFRESH is gating
		// the catalog refresh
		return nil, errSkipCatalogRefreshWhenTesting
	}

	perfTimings := timings.New(map[string]string{"ensure": "refresh-catalogs"})

	if err := os.MkdirAll(dirs.SnapCacheDir, 0755); err != nil {
		return nil, fmt.Errorf("cannot create directory %q: %v", dirs.SnapCacheDir, err)
	}

	var sections []string
	var err error
	timings.Run(perfTimings, "get-sections", "query store for sections", func(tm timings.Measurer) {
		sections, err = theStore.Sections(ctx, nil)
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(sections)
	if err := osutil.AtomicWriteFile(dirs.SnapSectionsFile, []byte(strings.Join(sections, "\n")), 0644, 0); err != nil {
		return nil, err
	}

	namesFile, err := osutil.NewAtomicFile(dirs.SnapNamesFile, 0644, 0, osutil.NoChown, osutil.NoChown)
	if err != nil {
		return nil, err
	}
	defer namesFile.Cancel()

	cmdDB, err := newCmdDB()
	if err != nil {
		return nil, err
	}

	// if all goes well we'll Commit() making this a NOP:
	defer cmdDB.Rollback()

	timings.Run(perfTimings, "write-catalogs", "query store for catalogs", func(tm timings.Measurer) {
		err = theStore.WriteCatalogs(ctx, namesFile, cmdDB)
	})
	if err != nil {
		return nil, err
	}

	err1 := namesFile.Commit()
	err2 := cmdDB.Commit()

	return perfTimings, strutil.JoinErrors(err1, err2)
}
