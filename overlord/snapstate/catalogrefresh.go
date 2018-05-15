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
)

var catalogRefreshDelay = 24 * time.Hour

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

	// sneakily don't do anything if in testing
	if CanAutoRefresh == nil {
		return nil
	}

	theStore := Store(r.state)
	now := time.Now()
	needsRefresh := r.nextCatalogRefresh.IsZero() || r.nextCatalogRefresh.Before(now)

	if !needsRefresh {
		return nil
	}

	next := now.Add(catalogRefreshDelay)
	// catalog refresh does not carry on trying on error
	r.nextCatalogRefresh = next

	logger.Debugf("Catalog refresh starting now; next scheduled for %s.", next)

	return refreshCatalogs(r.state, theStore)
}

var newCmdDB = advisor.Create

func refreshCatalogs(st *state.State, theStore StoreService) error {
	st.Unlock()
	defer st.Lock()

	if err := os.MkdirAll(dirs.SnapCacheDir, 0755); err != nil {
		return fmt.Errorf("cannot create directory %q: %v", dirs.SnapCacheDir, err)
	}

	sections, err := theStore.Sections(auth.EnsureContextTODO(), nil)
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

	if err := theStore.WriteCatalogs(auth.EnsureContextTODO(), namesFile, cmdDB); err != nil {
		return err
	}

	err1 := namesFile.Commit()
	err2 := cmdDB.Commit()

	if err2 != nil {
		return err2
	}

	return err1
}
