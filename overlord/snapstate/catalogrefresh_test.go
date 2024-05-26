// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017-2018 Canonical Ltd
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

package snapstate_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/advisor"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snapdenv"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/store/storetest"
	"github.com/snapcore/snapd/testutil"
)

type catalogStore struct {
	storetest.Store

	ops     []string
	tooMany bool
}

func (r *catalogStore) WriteCatalogs(ctx context.Context, w io.Writer, a store.SnapAdder) error {
	if ctx == nil || !auth.IsEnsureContext(ctx) {
		panic("Ensure marked context required")
	}
	r.ops = append(r.ops, "write-catalog")
	if r.tooMany {
		return store.ErrTooManyRequests
	}
	w.Write([]byte("pkg1\npkg2"))
	a.AddSnap("foo", "1.0", "foo summary", []string{"foo", "meh"})
	a.AddSnap("bar", "2.0", "bar summray", []string{"bar", "meh"})
	return nil
}

func (r *catalogStore) Sections(ctx context.Context, _ *auth.UserState) ([]string, error) {
	if ctx == nil || !auth.IsEnsureContext(ctx) {
		panic("Ensure marked context required")
	}
	r.ops = append(r.ops, "sections")
	if r.tooMany {
		return nil, store.ErrTooManyRequests
	}
	return []string{"section1", "section2"}, nil
}

type catalogRefreshTestSuite struct {
	state *state.State

	store  *catalogStore
	tmpdir string

	testutil.BaseTest
}

var _ = Suite(&catalogRefreshTestSuite{})

func (s *catalogRefreshTestSuite) SetUpTest(c *C) {
	s.tmpdir = c.MkDir()
	dirs.SetRootDir(s.tmpdir)
	s.state = state.New(nil)
	s.store = &catalogStore{}
	s.state.Lock()
	defer s.state.Unlock()
	snapstate.ReplaceStore(s.state, s.store)
	// mark system as seeded
	s.state.Set("seeded", true)

	// setup a simple deviceCtx since we check that for install mode
	s.AddCleanup(snapstatetest.MockDeviceModel(DefaultModel()))

	snapstate.CanAutoRefresh = func(*state.State) (bool, error) { return true, nil }
	s.AddCleanup(func() { snapstate.CanAutoRefresh = nil })
}

func (s *catalogRefreshTestSuite) TestCatalogRefresh(c *C) {
	// start with no catalog
	c.Check(dirs.SnapSectionsFile, testutil.FileAbsent)
	c.Check(dirs.SnapNamesFile, testutil.FileAbsent)
	c.Check(dirs.SnapCommandsDB, testutil.FileAbsent)

	cr7 := snapstate.NewCatalogRefresh(s.state)
	// next is initially zero
	c.Check(snapstate.NextCatalogRefresh(cr7).IsZero(), Equals, true)
	t0 := time.Now()
	mylog.Check(cr7.Ensure())
	c.Check(err, IsNil)

	// next now has a delta (next refresh is not before t0 + delta)
	c.Check(snapstate.NextCatalogRefresh(cr7).Before(t0.Add(snapstate.CatalogRefreshDelayWithDelta)), Equals, false)

	c.Check(s.store.ops, DeepEquals, []string{"sections", "write-catalog"})

	c.Check(osutil.FileExists(dirs.SnapSectionsFile), Equals, true)
	c.Check(dirs.SnapSectionsFile, testutil.FileEquals, "section1\nsection2")

	c.Check(osutil.FileExists(dirs.SnapNamesFile), Equals, true)
	c.Check(dirs.SnapNamesFile, testutil.FileEquals, "pkg1\npkg2")

	c.Check(osutil.FileExists(dirs.SnapCommandsDB), Equals, true)
	dump := mylog.Check2(advisor.DumpCommands())
	if errors.Is(err, advisor.ErrNotSupported) {
		c.Skip("bolt is not supported")
	}

	c.Check(dump, DeepEquals, map[string]string{
		"foo": `[{"snap":"foo","version":"1.0"}]`,
		"bar": `[{"snap":"bar","version":"2.0"}]`,
		"meh": `[{"snap":"foo","version":"1.0"},{"snap":"bar","version":"2.0"}]`,
	})
}

func (s *catalogRefreshTestSuite) TestCatalogRefreshTooMany(c *C) {
	s.store.tooMany = true

	cr7 := snapstate.NewCatalogRefresh(s.state)
	// next is initially zero
	c.Check(snapstate.NextCatalogRefresh(cr7).IsZero(), Equals, true)
	t0 := time.Now()
	mylog.Check(cr7.Ensure())
	if errors.Is(err, advisor.ErrNotSupported) {
		c.Skip("bolt is not supported")
	}
	c.Check(err, IsNil) // !!

	// next now has a delta (next refresh is not before t0 + delta)
	c.Check(snapstate.NextCatalogRefresh(cr7).Before(t0.Add(snapstate.CatalogRefreshDelayWithDelta)), Equals, false)

	// it tried one endpoint and bailed at the first 429
	c.Check(s.store.ops, HasLen, 1)

	// nothing got created
	c.Check(osutil.FileExists(dirs.SnapSectionsFile), Equals, false)
	c.Check(osutil.FileExists(dirs.SnapNamesFile), Equals, false)
	c.Check(osutil.FileExists(dirs.SnapCommandsDB), Equals, false)
}

func (s *catalogRefreshTestSuite) TestCatalogRefreshNotNeeded(c *C) {
	cr7 := snapstate.NewCatalogRefresh(s.state)
	snapstate.MockCatalogRefreshNextRefresh(cr7, time.Now().Add(1*time.Hour))
	mylog.Check(cr7.Ensure())
	c.Check(err, IsNil)
	c.Check(s.store.ops, HasLen, 0)
	c.Check(osutil.FileExists(dirs.SnapSectionsFile), Equals, false)
	c.Check(osutil.FileExists(dirs.SnapNamesFile), Equals, false)
}

func (s *catalogRefreshTestSuite) TestCatalogRefreshNewEnough(c *C) {
	// write a fake sections file just to have it
	c.Assert(os.MkdirAll(filepath.Dir(dirs.SnapNamesFile), 0755), IsNil)
	c.Assert(os.WriteFile(dirs.SnapNamesFile, nil, 0644), IsNil)
	// set the timestamp to something known
	t0 := time.Now().Truncate(time.Hour)
	c.Assert(os.Chtimes(dirs.SnapNamesFile, t0, t0), IsNil)

	cr7 := snapstate.NewCatalogRefresh(s.state)
	// next is initially zero
	c.Check(snapstate.NextCatalogRefresh(cr7).IsZero(), Equals, true)
	mylog.Check(cr7.Ensure())

	c.Check(s.store.ops, HasLen, 0)
	next := snapstate.NextCatalogRefresh(cr7)
	// next is no longer zero,
	c.Check(next.IsZero(), Equals, false)
	// but has a delta WRT the timestamp
	c.Check(next.Equal(t0.Add(snapstate.CatalogRefreshDelayWithDelta)), Equals, true)
}

func (s *catalogRefreshTestSuite) TestCatalogRefreshTooNew(c *C) {
	// write a fake sections file just to have it
	c.Assert(os.MkdirAll(filepath.Dir(dirs.SnapNamesFile), 0755), IsNil)
	c.Assert(os.WriteFile(dirs.SnapNamesFile, nil, 0644), IsNil)
	// but set the timestamp in the future
	t := time.Now().Add(time.Hour)
	c.Assert(os.Chtimes(dirs.SnapNamesFile, t, t), IsNil)

	cr7 := snapstate.NewCatalogRefresh(s.state)
	mylog.Check(cr7.Ensure())
	if errors.Is(err, advisor.ErrNotSupported) {
		c.Skip("bolt is not supported")
	}
	c.Check(err, IsNil)
	c.Check(s.store.ops, DeepEquals, []string{"sections", "write-catalog"})
}

func (s *catalogRefreshTestSuite) TestCatalogRefreshUnSeeded(c *C) {
	// mark system as unseeded (first boot)
	s.state.Lock()
	s.state.Set("seeded", nil)
	s.state.Unlock()

	cr7 := snapstate.NewCatalogRefresh(s.state)
	// next is initially zero
	c.Check(snapstate.NextCatalogRefresh(cr7).IsZero(), Equals, true)
	mylog.Check(cr7.Ensure())


	// next should be still zero as we skipped refresh on unseeded system
	c.Check(snapstate.NextCatalogRefresh(cr7).IsZero(), Equals, true)
	// nothing got created
	c.Check(osutil.FileExists(dirs.SnapSectionsFile), Equals, false)
	c.Check(osutil.FileExists(dirs.SnapNamesFile), Equals, false)
	c.Check(osutil.FileExists(dirs.SnapCommandsDB), Equals, false)
}

func (s *catalogRefreshTestSuite) TestCatalogRefreshUC20InstallMode(c *C) {
	// mark system as being in install mode
	trivialInstallDevice := &snapstatetest.TrivialDeviceContext{
		DeviceModel: DefaultModel(),
		SysMode:     "install",
	}

	r := snapstatetest.MockDeviceContext(trivialInstallDevice)
	defer r()

	cr7 := snapstate.NewCatalogRefresh(s.state)
	// next is initially zero
	c.Check(snapstate.NextCatalogRefresh(cr7).IsZero(), Equals, true)
	mylog.Check(cr7.Ensure())


	// next should be still zero as we skipped refresh on unseeded system
	c.Check(snapstate.NextCatalogRefresh(cr7).IsZero(), Equals, true)
	// nothing got created
	c.Check(osutil.FileExists(dirs.SnapSectionsFile), Equals, false)
	c.Check(osutil.FileExists(dirs.SnapNamesFile), Equals, false)
	c.Check(osutil.FileExists(dirs.SnapCommandsDB), Equals, false)
}

func (s *catalogRefreshTestSuite) TestCatalogRefreshSkipWhenTesting(c *C) {
	restore := snapdenv.MockTesting(true)
	defer restore()
	// catalog refresh disabled
	os.Setenv("SNAPD_CATALOG_REFRESH", "0")
	defer os.Unsetenv("SNAPD_CATALOG_REFRESH")

	// start with no catalog
	c.Check(dirs.SnapSectionsFile, testutil.FileAbsent)
	c.Check(dirs.SnapNamesFile, testutil.FileAbsent)
	c.Check(dirs.SnapCommandsDB, testutil.FileAbsent)

	cr7 := snapstate.NewCatalogRefresh(s.state)
	// next is initially zero
	c.Check(snapstate.NextCatalogRefresh(cr7).IsZero(), Equals, true)
	mylog.Check(cr7.Ensure())
	c.Check(err, IsNil)

	c.Check(s.store.ops, HasLen, 0)

	c.Check(dirs.SnapSectionsFile, testutil.FileAbsent)
	c.Check(dirs.SnapNamesFile, testutil.FileAbsent)
	c.Check(dirs.SnapCommandsDB, testutil.FileAbsent)

	// allow the refresh now
	os.Setenv("SNAPD_CATALOG_REFRESH", "1")

	// and reset the next refresh time
	snapstate.MockCatalogRefreshNextRefresh(cr7, time.Time{})
	// validity
	c.Check(snapstate.NextCatalogRefresh(cr7).IsZero(), Equals, true)
	mylog.Check(cr7.Ensure())
	if errors.Is(err, advisor.ErrNotSupported) {
		c.Skip("bolt is not supported")
	}
	c.Check(err, IsNil)

	// refresh happened
	c.Check(s.store.ops, DeepEquals, []string{"sections", "write-catalog"})

	c.Check(dirs.SnapSectionsFile, testutil.FilePresent)
	c.Check(dirs.SnapNamesFile, testutil.FilePresent)
	c.Check(dirs.SnapCommandsDB, testutil.FilePresent)
}

func (s *catalogRefreshTestSuite) TestSnapStoreOffline(c *C) {
	setStoreAccess(s.state, "offline")

	af := snapstate.NewCatalogRefresh(s.state)
	mylog.Check(af.Ensure())
	c.Check(err, IsNil)

	c.Check(s.store.ops, HasLen, 0)

	setStoreAccess(s.state, nil)
	mylog.Check(af.Ensure())
	if errors.Is(err, advisor.ErrNotSupported) {
		c.Skip("bolt is not supported")
	}

	c.Check(err, IsNil)

	c.Check(s.store.ops, DeepEquals, []string{"sections", "write-catalog"})
}
