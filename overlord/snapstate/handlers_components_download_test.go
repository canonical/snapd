package snapstate_test

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/store/storetest"

	. "gopkg.in/check.v1"
)

type downloadComponentSuite struct {
	baseHandlerSuite

	fakeStore *fakeStore
}

var _ = Suite(&downloadComponentSuite{})

func (s *downloadComponentSuite) SetUpTest(c *C) {
	s.baseHandlerSuite.SetUpTest(c)

	s.fakeStore = &fakeStore{
		state:       s.state,
		fakeBackend: s.fakeBackend,
	}

	s.state.Lock()
	defer s.state.Unlock()

	s.AddCleanup(snapstatetest.UseFallbackDeviceModel())

	snapstate.ReplaceStore(s.state, s.fakeStore)
	s.state.Set("refresh-privacy-key", "privacy-key")
}

func (s *downloadComponentSuite) TestDoDownloadComponent(c *C) {
	s.testDoDownloadComponent(c, testDoDownloadComponentOpts{})
}

func (s *downloadComponentSuite) TestDoDownloadComponentAutoRefresh(c *C) {
	s.testDoDownloadComponent(c, testDoDownloadComponentOpts{
		autoRefresh: true,
	})
}

func (s *downloadComponentSuite) TestDoDownloadComponentCustomBlobDir(c *C) {
	s.testDoDownloadComponent(c, testDoDownloadComponentOpts{
		blobDir: c.MkDir(),
	})
}

type testDoDownloadComponentOpts struct {
	autoRefresh bool
	blobDir     string
}

func (s *downloadComponentSuite) testDoDownloadComponent(c *C, opts testDoDownloadComponentOpts) {
	s.state.Lock()
	defer s.state.Unlock()

	// set auto-refresh rate-limit
	tr := config.NewTransaction(s.state)
	tr.Set("core", "refresh.rate-limit", "1234B")
	tr.Commit()

	si := &snap.SideInfo{
		RealName: "snap",
		SnapID:   snaptest.AssertedSnapID("snap"),
		Revision: snap.R(11),
		Channel:  "latest/stable",
	}

	t := s.state.NewTask("download-component", "...")

	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo:        si,
		InstanceKey:     "key",
		Flags:           snapstate.Flags{IsAutoRefresh: opts.autoRefresh},
		DownloadBlobDir: opts.blobDir,
	})

	t.Set("component-setup", &snapstate.ComponentSetup{
		CompSideInfo: snap.NewComponentSideInfo(
			naming.NewComponentRef("snap", "comp"),
			snap.R(11),
		),
		CompType: snap.StandardComponent,
		DownloadInfo: &snap.DownloadInfo{
			DownloadURL: "http://some-url.com/comp",
		},
		DownloadBlobDir: opts.blobDir,
	})

	chg := s.state.NewChange("download", "...")
	chg.AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()

	c.Assert(chg.Err(), IsNil)

	// only the download endpoint of the store was hit
	c.Check(s.fakeBackend.ops, DeepEquals, fakeOps{
		{
			op:   "storesvc-download",
			name: "snap+comp",
		},
	})

	blobDir := opts.blobDir
	if blobDir == "" {
		blobDir = dirs.SnapBlobDir
	}
	expectedPath := filepath.Join(blobDir, "snap_key+comp_11.comp")

	var compsup snapstate.ComponentSetup
	err := t.Get("component-setup", &compsup)
	c.Assert(err, IsNil)
	c.Check(compsup.CompPath, Equals, expectedPath)
	c.Check(t.Status(), Equals, state.DoneStatus)

	var downloadOpts *store.DownloadOptions
	if opts.autoRefresh {
		downloadOpts = &store.DownloadOptions{
			RateLimit: 1234,
			Scheduled: true,
		}
	}

	c.Check(s.fakeStore.downloads, DeepEquals, []fakeDownload{
		{
			name:   "snap+comp",
			target: expectedPath,
			opts:   downloadOpts,
		},
	})
}

type downloadErrorStore struct {
	storetest.Store
}

func (*downloadErrorStore) Download(context.Context, string, string, *snap.DownloadInfo, progress.Meter, *auth.UserState, *store.DownloadOptions) error {
	return errors.New("download failed")
}

func (s *downloadComponentSuite) TestDoDownloadComponentFailure(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.ReplaceStore(s.state, &downloadErrorStore{})

	si := &snap.SideInfo{
		RealName: "snap",
		SnapID:   snaptest.AssertedSnapID("snap"),
		Revision: snap.R(11),
		Channel:  "latest/stable",
	}

	t := s.state.NewTask("download-component", "...")

	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo:    si,
		InstanceKey: "key",
	})

	t.Set("component-setup", &snapstate.ComponentSetup{
		CompSideInfo: snap.NewComponentSideInfo(
			naming.NewComponentRef("snap", "comp"),
			snap.R(11),
		),
		CompType: snap.StandardComponent,
		DownloadInfo: &snap.DownloadInfo{
			DownloadURL: "http://some-url.com/comp",
		},
	})

	chg := s.state.NewChange("download", "...")
	chg.AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()

	c.Assert(chg.Err(), ErrorMatches, `(?s).*cannot download component "comp": download failed.*`)

	// only the download endpoint of the store was hit
	c.Check(s.fakeBackend.ops, HasLen, 0)

	// make sure we didn't set a path on the component setup
	var compsup snapstate.ComponentSetup
	err := t.Get("component-setup", &compsup)
	c.Assert(err, IsNil)
	c.Check(compsup.CompPath, Equals, "")
}
