package snapstate_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/overlord/snapstate/sequence"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/store"
	. "gopkg.in/check.v1"
)

type targetTestSuite struct {
	snapmgrBaseTest
}

var _ = Suite(&targetTestSuite{})

func (s *targetTestSuite) TestInstallWithComponents(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	const (
		snapName = "some-snap"
		compName = "standard-component"
		channel  = "channel-for-components"
	)
	s.fakeStore.snapResourcesFn = func(info *snap.Info) []store.SnapResourceResult {
		c.Assert(info.SnapName(), DeepEquals, snapName)

		return []store.SnapResourceResult{
			{
				DownloadInfo: snap.DownloadInfo{
					DownloadURL: fmt.Sprintf("http://example.com/%s", snapName),
				},
				Name:      compName,
				Revision:  1,
				Type:      fmt.Sprintf("component/%s", snap.StandardComponent),
				Version:   "1.0",
				CreatedAt: "2024-01-01T00:00:00Z",
			},
		}
	}

	goal := snapstate.StoreInstallGoal(snapstate.StoreSnap{
		InstanceName: snapName,
		Components:   []string{compName},
		RevOpts: snapstate.RevisionOptions{
			Channel: channel,
		},
	})

	info, ts, err := snapstate.InstallOne(context.Background(), s.state, goal, snapstate.Options{})
	c.Assert(err, IsNil)

	c.Check(info.InstanceName(), Equals, snapName)
	c.Check(info.Channel, Equals, channel)
	c.Check(info.Components[compName].Name, Equals, compName)

	verifyInstallTasksWithComponents(c, snap.TypeApp, 0, 0, []string{compName}, ts)

	setupTask := ts.Tasks()[1]

	compsups, err := snapstate.TaskComponentSetups(setupTask)
	c.Assert(err, IsNil)
	c.Assert(compsups, HasLen, 1)
	c.Check(compsups[0].CompSideInfo.Component.ComponentName, Equals, compName)
}

func (s *targetTestSuite) TestInstallWithComponentsMissingResource(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	const (
		snapName = "some-snap"
		compName = "standard-component"
		channel  = "channel-for-components"
	)
	s.fakeStore.snapResourcesFn = func(info *snap.Info) []store.SnapResourceResult {
		c.Assert(info.SnapName(), DeepEquals, snapName)

		return []store.SnapResourceResult{
			{
				DownloadInfo: snap.DownloadInfo{
					DownloadURL: fmt.Sprintf("http://example.com/%s", snapName),
				},
				Name:      "missing-component",
				Revision:  1,
				Type:      fmt.Sprintf("component/%s", snap.StandardComponent),
				Version:   "1.0",
				CreatedAt: "2024-01-01T00:00:00Z",
			},
		}
	}

	goal := snapstate.StoreInstallGoal(snapstate.StoreSnap{
		InstanceName: snapName,
		Components:   []string{compName},
		RevOpts: snapstate.RevisionOptions{
			Channel: channel,
		},
	})

	_, _, err := snapstate.InstallOne(context.Background(), s.state, goal, snapstate.Options{})
	c.Assert(err, ErrorMatches, fmt.Sprintf(`.*cannot find component "%s" in snap resources`, compName))
}

func (s *targetTestSuite) TestInstallWithComponentsWrongType(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	const (
		snapName = "some-snap"
		compName = "standard-component"
		channel  = "channel-for-components"
	)
	s.fakeStore.snapResourcesFn = func(info *snap.Info) []store.SnapResourceResult {
		c.Assert(info.SnapName(), DeepEquals, snapName)

		return []store.SnapResourceResult{
			{
				DownloadInfo: snap.DownloadInfo{
					DownloadURL: fmt.Sprintf("http://example.com/%s", snapName),
				},
				Name:      compName,
				Revision:  1,
				Type:      fmt.Sprintf("component/%s", snap.KernelModulesComponent),
				Version:   "1.0",
				CreatedAt: "2024-01-01T00:00:00Z",
			},
		}
	}

	goal := snapstate.StoreInstallGoal(snapstate.StoreSnap{
		InstanceName: snapName,
		Components:   []string{compName},
		RevOpts: snapstate.RevisionOptions{
			Channel: channel,
		},
	})

	_, _, err := snapstate.InstallOne(context.Background(), s.state, goal, snapstate.Options{})
	c.Assert(err, ErrorMatches, fmt.Sprintf(
		`.*inconsistent component type \("%s" in snap, "%s" in component\)`, snap.StandardComponent, snap.KernelModulesComponent,
	))
}

func (s *targetTestSuite) TestInstallWithComponentsOtherResource(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	const (
		snapName = "test-snap"
		compName = "standard-component"
		channel  = "channel-for-components"
	)
	s.fakeStore.snapResourcesFn = func(info *snap.Info) []store.SnapResourceResult {
		c.Assert(info.SnapName(), DeepEquals, snapName)

		return []store.SnapResourceResult{
			{
				DownloadInfo: snap.DownloadInfo{
					DownloadURL: fmt.Sprintf("http://example.com/%s", snapName),
				},
				Name:      compName,
				Revision:  1,
				Type:      "otherresource/restype",
				Version:   "1.0",
				CreatedAt: "2024-01-01T00:00:00Z",
			},
		}
	}

	goal := snapstate.StoreInstallGoal(snapstate.StoreSnap{
		InstanceName: snapName,
		Components:   []string{compName},
		RevOpts: snapstate.RevisionOptions{
			Channel: channel,
		},
	})

	_, _, err := snapstate.InstallOne(context.Background(), s.state, goal, snapstate.Options{})
	c.Assert(err, ErrorMatches, fmt.Sprintf(
		`.*"otherresource/restype" is not a component resource`))
}

func (s *targetTestSuite) TestInstallWithComponentsMissingInInfo(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	const (
		snapName = "some-snap"
		compName = "test-missing-component"
		channel  = "channel-for-components"
	)
	s.fakeStore.snapResourcesFn = func(info *snap.Info) []store.SnapResourceResult {
		c.Assert(info.SnapName(), DeepEquals, snapName)

		return []store.SnapResourceResult{
			{
				DownloadInfo: snap.DownloadInfo{
					DownloadURL: fmt.Sprintf("http://example.com/%s", snapName),
				},
				Name:      compName,
				Revision:  1,
				Type:      fmt.Sprintf("component/%s", snap.StandardComponent),
				Version:   "1.0",
				CreatedAt: "2024-01-01T00:00:00Z",
			},
		}
	}

	goal := snapstate.StoreInstallGoal(snapstate.StoreSnap{
		InstanceName: snapName,
		Components:   []string{compName},
		RevOpts: snapstate.RevisionOptions{
			Channel: channel,
		},
	})

	_, _, err := snapstate.InstallOne(context.Background(), s.state, goal, snapstate.Options{})
	c.Assert(err, ErrorMatches, fmt.Sprintf(`.*"%s" is not a component for snap "%s"`, compName, snapName))
}

func (s *targetTestSuite) TestInstallWithComponentsFromPath(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	const (
		snapName = "some-snap"
		snapID   = "some-snap-id"
		compName = "standard-component"
		snapYaml = `name: some-snap
type: kernel
version: 1.0
components:
  standard-component:
    type: standard
  kernel-modules-component:
    type: kernel-modules
`
		componentYaml = `component: some-snap+standard-component
type: standard
version: 1.0
`
	)

	snapRevision := snap.R(2)
	si := &snap.SideInfo{
		RealName: snapName,
		SnapID:   snapID,
		Revision: snapRevision,
	}
	snapPath := makeTestSnap(c, snapYaml)

	csi := snap.ComponentSideInfo{
		Component: naming.NewComponentRef(snapName, compName),
		Revision:  snap.R(3),
	}

	components := []snapstate.PathComponent{{
		SideInfo: &csi,
		Path:     snaptest.MakeTestComponent(c, componentYaml),
	}}

	goal := snapstate.PathInstallGoal(snapstate.PathSnap{
		Path:       snapPath,
		SideInfo:   si,
		Components: components,
	})

	info, ts, err := snapstate.InstallOne(context.Background(), s.state, goal, snapstate.Options{})
	c.Assert(err, IsNil)

	c.Check(info.InstanceName(), Equals, snapName)
	c.Check(info.Components[compName].Name, Equals, compName)

	verifyInstallTasksWithComponents(c, snap.TypeKernel, localSnap|updatesGadgetAssets, 0, []string{compName}, ts)
}

func (s *targetTestSuite) TestInstallWithComponentsMixedAssertedCompsAndUnassertedSnap(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	const (
		snapName = "some-snap"
		compName = "standard-component"
		snapYaml = `name: some-snap
version: 1.0
type: kernel
components:
  standard-component:
    type: standard
  kernel-modules-component:
    type: kernel-modules
`
		componentYaml = `component: some-snap+standard-component
type: standard
version: 1.0
`
	)

	snapRevision := snap.Revision{}
	si := &snap.SideInfo{
		RealName: snapName,
		Revision: snapRevision,
	}
	snapPath := makeTestSnap(c, snapYaml)

	csi := snap.ComponentSideInfo{
		Component: naming.NewComponentRef(snapName, compName),
		Revision:  snap.R(3),
	}

	components := []snapstate.PathComponent{{
		SideInfo: &csi,
		Path:     snaptest.MakeTestComponent(c, componentYaml),
	}}

	goal := snapstate.PathInstallGoal(snapstate.PathSnap{
		Path:       snapPath,
		SideInfo:   si,
		Components: components,
	})

	_, _, err := snapstate.InstallOne(context.Background(), s.state, goal, snapstate.Options{})
	c.Assert(err, ErrorMatches, "cannot mix unasserted snap and asserted components")
}

func (s *targetTestSuite) TestInstallWithComponentsMixedUnassertedCompsAndAssertedSnap(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	const (
		snapName = "some-snap"
		snapID   = "some-snap-id"
		compName = "standard-component"
		snapYaml = `name: some-snap
version: 1.0
type: kernel
components:
  standard-component:
    type: standard
  kernel-modules-component:
    type: kernel-modules
`
		componentYaml = `component: some-snap+standard-component
type: standard
version: 1.0
`
	)

	snapRevision := snap.R(2)
	si := &snap.SideInfo{
		RealName: snapName,
		SnapID:   snapID,
		Revision: snapRevision,
	}
	snapPath := makeTestSnap(c, snapYaml)

	csi := snap.ComponentSideInfo{
		Component: naming.NewComponentRef(snapName, compName),
		Revision:  snap.Revision{},
	}

	components := []snapstate.PathComponent{{
		SideInfo: &csi,
		Path:     snaptest.MakeTestComponent(c, componentYaml),
	}}

	goal := snapstate.PathInstallGoal(snapstate.PathSnap{
		Path:       snapPath,
		SideInfo:   si,
		Components: components,
	})

	_, _, err := snapstate.InstallOne(context.Background(), s.state, goal, snapstate.Options{})
	c.Assert(err, ErrorMatches, "cannot mix asserted snap and unasserted components")
}

func (s *targetTestSuite) TestUpdateSnapNotInstalled(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	goal := snapstate.StoreUpdateGoal(snapstate.StoreUpdate{
		InstanceName: "some-snap",
		RevOpts: snapstate.RevisionOptions{
			Channel: "some-channel",
		},
	})

	_, err := snapstate.UpdateOne(context.Background(), s.state, goal, nil, snapstate.Options{})
	c.Assert(err, ErrorMatches, `snap "some-snap" is not installed`)
}

func (s *targetTestSuite) TestInvalidPathGoals(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	type test struct {
		snap snapstate.PathSnap
		err  string
	}

	tests := []test{
		{
			snap: snapstate.PathSnap{
				SideInfo: &snap.SideInfo{},
				Path:     "some-path",
			},
			err: `internal error: snap name to install "some-path" not provided`,
		},
		{
			snap: snapstate.PathSnap{
				SideInfo: &snap.SideInfo{
					RealName: "some-snap",
					SnapID:   "some-snap-id",
				},
				Path: "some-path",
			},
			err: `internal error: snap id set to install "some-path" but revision is unset`,
		},
		{
			snap: snapstate.PathSnap{
				SideInfo: &snap.SideInfo{
					RealName: "some+snap",
				},
			},
			err: `invalid instance name: invalid snap name: "some\+snap"`,
		},
		{
			snap: snapstate.PathSnap{
				SideInfo: &snap.SideInfo{
					RealName: "some-snap",
					Revision: snap.R(1),
				},
				RevOpts: snapstate.RevisionOptions{
					Revision: snap.R(2),
				},
			},
			err: `cannot install local snap "some-snap": 2 != 1 \(revision mismatch\)`,
		},
	}

	for _, t := range tests {
		update := snapstate.PathUpdateGoal(t.snap)
		_, err := snapstate.UpdateOne(context.Background(), s.state, update, nil, snapstate.Options{})
		c.Check(err, ErrorMatches, t.err)

		install := snapstate.PathInstallGoal(snapstate.PathSnap{
			InstanceName: t.snap.InstanceName,
			Path:         t.snap.Path,
			SideInfo:     t.snap.SideInfo,
			RevOpts:      t.snap.RevOpts,
		})
		_, _, err = snapstate.InstallOne(context.Background(), s.state, install, snapstate.Options{})
		c.Check(err, ErrorMatches, t.err)
	}
}

func (s *targetTestSuite) TestInstallFromStoreDefaultChannel(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	goal := snapstate.StoreInstallGoal(snapstate.StoreSnap{
		InstanceName: "some-snap",
	})

	info, ts, err := snapstate.InstallOne(context.Background(), s.state, goal, snapstate.Options{})
	c.Assert(err, IsNil)

	c.Check(info.InstanceName(), Equals, "some-snap")
	c.Check(info.Channel, Equals, "stable")

	snapsup, err := snapstate.TaskSnapSetup(ts.Tasks()[0])
	c.Assert(err, IsNil)
	c.Check(snapsup.Channel, Equals, "stable")
}

func (s *targetTestSuite) TestInstallFromPathDefaultChannel(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapPath := makeTestSnap(c, `name: some-snap
version: 1.0
components:
  standard-component:
    type: standard
`)
	si := &snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(1),
	}

	goal := snapstate.PathInstallGoal(snapstate.PathSnap{
		InstanceName: si.RealName,
		Path:         snapPath,
		SideInfo:     si,
	})

	info, ts, err := snapstate.InstallOne(context.Background(), s.state, goal, snapstate.Options{})
	c.Assert(err, IsNil)

	c.Check(info.InstanceName(), Equals, "some-snap")
	c.Check(info.Channel, Equals, "")

	snapsup, err := snapstate.TaskSnapSetup(ts.Tasks()[0])
	c.Assert(err, IsNil)
	c.Check(snapsup.Channel, Equals, "")
}

func (s *targetTestSuite) TestInstallComponentsFromPathInvalidComponentFile(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// use the real thing for this one
	snapstate.MockOpenSnapFile(backend.OpenSnapFile)

	const (
		snapID        = "test-snap-id"
		snapName      = "test-snap"
		componentName = "standard-component"
	)
	snapRevision := snap.R(11)

	csi := snap.ComponentSideInfo{
		Component: naming.NewComponentRef(snapName, componentName),
		Revision:  snap.R(1),
	}

	compPath := filepath.Join(c.MkDir(), "invalid-component")
	err := os.WriteFile(compPath, []byte("invalid-component"), 0644)
	c.Assert(err, IsNil)

	components := []snapstate.PathComponent{{
		SideInfo: &csi,
		Path:     compPath,
	}}

	snapPath := makeTestSnap(c, `name: test-snap
version: 1.0
components:
  standard-component:
    type: standard
`)
	si := &snap.SideInfo{
		RealName: snapName,
		SnapID:   snapID,
		Revision: snapRevision,
	}

	goal := snapstate.PathInstallGoal(snapstate.PathSnap{
		Path:       snapPath,
		SideInfo:   si,
		Components: components,
	})
	_, _, err = snapstate.InstallOne(context.Background(), s.state, goal, snapstate.Options{})
	c.Assert(err, ErrorMatches, fmt.Sprintf(`.*cannot process snap or snapdir: file "%s" is invalid.*`, compPath))
}

func (s *targetTestSuite) TestInstallFromPathSideInfoChannel(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapPath := makeTestSnap(c, `name: some-snap
version: 1.0
components:
  standard-component:
    type: standard
`)
	si := &snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(1),
		Channel:  "edge",
	}

	goal := snapstate.PathInstallGoal(snapstate.PathSnap{
		InstanceName: si.RealName,
		Path:         snapPath,
		SideInfo:     si,
	})

	info, ts, err := snapstate.InstallOne(context.Background(), s.state, goal, snapstate.Options{})
	c.Assert(err, IsNil)

	c.Check(info.InstanceName(), Equals, "some-snap")
	c.Check(info.Channel, Equals, "edge")

	snapsup, err := snapstate.TaskSnapSetup(ts.Tasks()[0])
	c.Assert(err, IsNil)
	c.Check(snapsup.Channel, Equals, "edge")
}

func (s *targetTestSuite) TestInstallFromPathRevOptsChannel(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapPath := makeTestSnap(c, `name: some-snap
version: 1.0
components:
  standard-component:
    type: standard
`)
	si := &snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(1),
	}

	goal := snapstate.PathInstallGoal(snapstate.PathSnap{
		Path:     snapPath,
		SideInfo: si,
		RevOpts:  snapstate.RevisionOptions{Channel: "edge"},
	})

	info, ts, err := snapstate.InstallOne(context.Background(), s.state, goal, snapstate.Options{})
	c.Assert(err, IsNil)

	c.Check(info.InstanceName(), Equals, "some-snap")

	// should be missing here, since the side info doesn't have a channel. we're
	// just setting the tracked channel in the revision options
	c.Check(info.Channel, Equals, "")

	snapsup, err := snapstate.TaskSnapSetup(ts.Tasks()[0])
	c.Assert(err, IsNil)
	c.Check(snapsup.Channel, Equals, "edge")
}

func (s *targetTestSuite) TestInstallFromPathRevOptsSideInfoChannelMismatch(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapPath := makeTestSnap(c, `name: some-snap
version: 1.0
components:
  standard-component:
    type: standard
`)
	si := &snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(1),
		Channel:  "stable",
	}

	goal := snapstate.PathInstallGoal(snapstate.PathSnap{
		Path:     snapPath,
		SideInfo: si,
		RevOpts:  snapstate.RevisionOptions{Channel: "edge"},
	})

	_, _, err := snapstate.InstallOne(context.Background(), s.state, goal, snapstate.Options{})
	c.Assert(err, ErrorMatches, `cannot install local snap "some-snap": edge != stable \(channel mismatch\)`)
}

func (s *targetTestSuite) TestInstallFromStoreRevisionAndChannel(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	goal := snapstate.StoreInstallGoal(snapstate.StoreSnap{
		InstanceName: "some-snap",
		RevOpts: snapstate.RevisionOptions{
			Channel:  "stable",
			Revision: snap.R(7),
		},
	})

	info, ts, err := snapstate.InstallOne(context.Background(), s.state, goal, snapstate.Options{})
	c.Assert(err, IsNil)

	c.Check(info.InstanceName(), Equals, "some-snap")
	c.Check(info.Channel, Equals, "stable")

	snapsup, err := snapstate.TaskSnapSetup(ts.Tasks()[0])
	c.Assert(err, IsNil)
	c.Check(snapsup.Channel, Equals, "stable")
	c.Check(snapsup.Revision(), Equals, snap.R(7))
}

func (s *targetTestSuite) TestInstallFromStoreRevisionAndChannelWithRedirectChannel(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	goal := snapstate.StoreInstallGoal(snapstate.StoreSnap{
		InstanceName: "some-snap-with-default-track",
		RevOpts: snapstate.RevisionOptions{
			Channel:  "stable",
			Revision: snap.R(7),
		},
	})

	info, ts, err := snapstate.InstallOne(context.Background(), s.state, goal, snapstate.Options{})
	c.Assert(err, IsNil)

	c.Check(info.InstanceName(), Equals, "some-snap-with-default-track")

	// note that this is the effective channel, not the tracked channel. this
	// doesn't have to be the same as the channel in the SnapSetup, and it is
	// really only here to let us know exactly where the snap came from.
	c.Check(info.Channel, Equals, "stable")

	snapsup, err := snapstate.TaskSnapSetup(ts.Tasks()[0])
	c.Assert(err, IsNil)
	c.Check(snapsup.Channel, Equals, "2.0/stable")
	c.Check(snapsup.Revision(), Equals, snap.R(7))
}

func (s *targetTestSuite) TestUpdateComponents(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	const (
		snapName = "some-snap"
		snapID   = "some-snap-id"
		compName = "standard-component"
		channel  = "channel-for-components"
	)

	seq := snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{
		RealName: snapName,
		SnapID:   snapID,
		Revision: snap.R(7),
	}})

	seq.AddComponentForRevision(snap.R(7), &sequence.ComponentState{
		SideInfo: &snap.ComponentSideInfo{
			Component: naming.NewComponentRef(snapName, compName),
			Revision:  snap.R(1),
		},
		CompType: snap.StandardComponent,
	})

	s.AddCleanup(snapstate.MockReadComponentInfo(func(
		compMntDir string, info *snap.Info, csi *snap.ComponentSideInfo,
	) (*snap.ComponentInfo, error) {
		return &snap.ComponentInfo{
			Component:         naming.NewComponentRef(info.SnapName(), compName),
			Type:              snap.StandardComponent,
			CompVersion:       "1.0",
			ComponentSideInfo: *csi,
		}, nil
	}))

	snapstate.Set(s.state, snapName, &snapstate.SnapState{
		Active:          true,
		TrackingChannel: channel,
		Sequence:        seq,
		Current:         snap.R(7),
		SnapType:        "app",
	})

	s.fakeStore.snapResourcesFn = func(info *snap.Info) []store.SnapResourceResult {
		c.Assert(info.SnapName(), DeepEquals, snapName)

		return []store.SnapResourceResult{
			{
				DownloadInfo: snap.DownloadInfo{
					DownloadURL: fmt.Sprintf("http://example.com/%s", snapName),
				},
				Name:      compName,
				Revision:  2,
				Type:      fmt.Sprintf("component/%s", snap.StandardComponent),
				Version:   "1.0",
				CreatedAt: "2024-01-01T00:00:00Z",
			},
		}
	}

	goal := snapstate.StoreUpdateGoal(snapstate.StoreUpdate{
		InstanceName: snapName,
	})

	ts, err := snapstate.UpdateOne(context.Background(), s.state, goal, nil, snapstate.Options{})
	c.Assert(err, IsNil)

	verifyUpdateTasksWithComponents(c, snap.TypeApp, doesReRefresh, 0, 0, []string{compName}, ts)
}

func (s *targetTestSuite) TestUpdateComponentsSameComponentRevision(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	const (
		snapName = "some-snap"
		snapID   = "some-snap-id"
		compName = "standard-component"
		channel  = "channel-for-components"
	)

	seq := snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{
		RealName: snapName,
		SnapID:   snapID,
		Revision: snap.R(7),
	}})

	seq.AddComponentForRevision(snap.R(7), &sequence.ComponentState{
		SideInfo: &snap.ComponentSideInfo{
			Component: naming.NewComponentRef(snapName, compName),
			Revision:  snap.R(1),
		},
		CompType: snap.StandardComponent,
	})

	s.AddCleanup(snapstate.MockReadComponentInfo(func(
		compMntDir string, info *snap.Info, csi *snap.ComponentSideInfo,
	) (*snap.ComponentInfo, error) {
		return &snap.ComponentInfo{
			Component:         naming.NewComponentRef(info.SnapName(), compName),
			Type:              snap.StandardComponent,
			CompVersion:       "1.0",
			ComponentSideInfo: *csi,
		}, nil
	}))

	snapstate.Set(s.state, snapName, &snapstate.SnapState{
		Active:          true,
		TrackingChannel: channel,
		Sequence:        seq,
		Current:         snap.R(7),
		SnapType:        "app",
	})

	storeAccessed := false
	s.fakeStore.snapResourcesFn = func(info *snap.Info) []store.SnapResourceResult {
		c.Assert(info.SnapName(), DeepEquals, snapName)
		storeAccessed = true
		return []store.SnapResourceResult{
			{
				DownloadInfo: snap.DownloadInfo{
					DownloadURL: fmt.Sprintf("http://example.com/%s", snapName),
				},
				Name:      compName,
				Revision:  1,
				Type:      fmt.Sprintf("component/%s", snap.StandardComponent),
				Version:   "1.0",
				CreatedAt: "2024-01-01T00:00:00Z",
			},
		}
	}

	goal := snapstate.StoreUpdateGoal(snapstate.StoreUpdate{
		InstanceName: snapName,
	})

	ts, err := snapstate.UpdateOne(context.Background(), s.state, goal, nil, snapstate.Options{})
	c.Assert(err, IsNil)

	verifyUpdateTasksWithComponents(c, snap.TypeApp, doesReRefresh, compOptRevisionPresent, 0, []string{compName}, ts)

	c.Assert(storeAccessed, Equals, true)
}

func (s *targetTestSuite) TestUpdateComponentsFromPath(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	const (
		snapName = "some-snap"
		snapID   = "some-snap-id"
		compName = "standard-component"
		channel  = "channel-for-components"
		snapYaml = `name: some-snap
type: kernel
version: 1.0
components:
  standard-component:
    type: standard
  kernel-modules-component:
    type: kernel-modules
epoch: 1
`
		componentYaml = `component: some-snap+standard-component
type: standard
version: 1.0
`
	)

	seq := snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{
		RealName: snapName,
		SnapID:   snapID,
		Revision: snap.R(7),
	}})

	seq.AddComponentForRevision(snap.R(7), &sequence.ComponentState{
		SideInfo: &snap.ComponentSideInfo{
			Component: naming.NewComponentRef(snapName, compName),
			Revision:  snap.R(1),
		},
		CompType: snap.StandardComponent,
	})

	snapstate.Set(s.state, snapName, &snapstate.SnapState{
		Active:          true,
		TrackingChannel: channel,
		Sequence:        seq,
		Current:         snap.R(7),
		SnapType:        "app",
	})

	si := &snap.SideInfo{
		RealName: snapName,
		SnapID:   snapID,
		Revision: snap.R(9),
	}
	snapPath := makeTestSnap(c, snapYaml)

	csi := snap.ComponentSideInfo{
		Component: naming.NewComponentRef(snapName, compName),
		Revision:  snap.R(2),
	}

	components := []snapstate.PathComponent{{
		SideInfo: &csi,
		Path:     snaptest.MakeTestComponent(c, componentYaml),
	}}

	goal := snapstate.PathUpdateGoal(snapstate.PathSnap{
		InstanceName: snapName,
		Path:         snapPath,
		SideInfo:     si,
		Components:   components,
	})

	ts, err := snapstate.UpdateOne(context.Background(), s.state, goal, nil, snapstate.Options{})
	c.Assert(err, IsNil)

	verifyUpdateTasksWithComponents(c, snap.TypeApp, doesReRefresh|localSnap|updatesGadgetAssets, 0, 0, []string{compName}, ts)
}

func (s *targetTestSuite) TestUpdateComponentsFromPathInvalidComponentFile(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	const (
		snapName = "some-snap"
		snapID   = "some-snap-id"
		compName = "standard-component"
		channel  = "channel-for-components"
		snapYaml = `name: some-snap
type: kernel
version: 1.0
components:
  standard-component:
    type: standard
  kernel-modules-component:
    type: kernel-modules
epoch: 1
`
		componentYaml = `component: some-snap+standard-component
type: standard
version: 1.0
`
	)

	seq := snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{
		RealName: snapName,
		SnapID:   snapID,
		Revision: snap.R(7),
	}})

	seq.AddComponentForRevision(snap.R(7), &sequence.ComponentState{
		SideInfo: &snap.ComponentSideInfo{
			Component: naming.NewComponentRef(snapName, compName),
			Revision:  snap.R(1),
		},
		CompType: snap.StandardComponent,
	})

	snapstate.Set(s.state, snapName, &snapstate.SnapState{
		Active:          true,
		TrackingChannel: channel,
		Sequence:        seq,
		Current:         snap.R(7),
		SnapType:        "app",
	})

	si := &snap.SideInfo{
		RealName: snapName,
		SnapID:   snapID,
		Revision: snap.R(9),
	}
	snapPath := makeTestSnap(c, snapYaml)

	csi := snap.ComponentSideInfo{
		Component: naming.NewComponentRef(snapName, compName),
		Revision:  snap.R(2),
	}

	compPath := filepath.Join(c.MkDir(), "invalid-component")
	err := os.WriteFile(compPath, []byte("invalid-component"), 0644)
	c.Assert(err, IsNil)

	components := []snapstate.PathComponent{{
		SideInfo: &csi,
		Path:     compPath,
	}}

	goal := snapstate.PathUpdateGoal(snapstate.PathSnap{
		InstanceName: snapName,
		Path:         snapPath,
		SideInfo:     si,
		Components:   components,
	})

	_, err = snapstate.UpdateOne(context.Background(), s.state, goal, nil, snapstate.Options{})
	c.Assert(err, ErrorMatches, fmt.Sprintf(`.*cannot process snap or snapdir: file "%s" is invalid.*`, compPath))
}

func (s *targetTestSuite) TestUpdateComponentsFromPathInvalidComponentName(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	const (
		snapName = "some-snap"
		snapID   = "some-snap-id"
		compName = "standard-component"
		snapYaml = `name: some-snap
type: kernel
version: 1.0
components:
  standard-component:
    type: standard
  kernel-modules-component:
    type: kernel-modules
epoch: 1
`
		componentYaml = `component: some-snap+standard-component
type: standard
version: 1.0
`
	)

	seq := snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{
		RealName: snapName,
		SnapID:   snapID,
		Revision: snap.R(7),
	}})

	seq.AddComponentForRevision(snap.R(7), &sequence.ComponentState{
		SideInfo: &snap.ComponentSideInfo{
			Component: naming.NewComponentRef(snapName, compName),
			Revision:  snap.R(1),
		},
		CompType: snap.StandardComponent,
	})

	snapstate.Set(s.state, snapName, &snapstate.SnapState{
		Active:   true,
		Sequence: seq,
		Current:  snap.R(7),
		SnapType: "app",
	})

	si := &snap.SideInfo{
		RealName: snapName,
		SnapID:   snapID,
		Revision: snap.R(9),
	}
	snapPath := makeTestSnap(c, snapYaml)

	badName := "Bad-component"
	csi := snap.ComponentSideInfo{
		Component: naming.NewComponentRef(snapName, badName),
		Revision:  snap.R(2),
	}

	compPath := filepath.Join(c.MkDir(), "invalid-component")
	err := os.WriteFile(compPath, []byte("invalid-component"), 0644)
	c.Assert(err, IsNil)

	components := []snapstate.PathComponent{{
		SideInfo: &csi,
		Path:     compPath,
	}}

	goal := snapstate.PathUpdateGoal(snapstate.PathSnap{
		InstanceName: snapName,
		Path:         snapPath,
		SideInfo:     si,
		Components:   components,
	})

	_, err = snapstate.UpdateOne(context.Background(), s.state, goal, nil, snapstate.Options{})
	c.Assert(err, ErrorMatches, fmt.Sprintf(`invalid snap name: "%s"`, badName))
}

func (s *targetTestSuite) TestUpdateComponentsFromPathInvalidMissingInInfo(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	const (
		snapName = "some-snap"
		snapID   = "some-snap-id"
		compName = "other-component"
		snapYaml = `name: some-snap
type: kernel
version: 1.0
components:
  standard-component:
    type: standard
  kernel-modules-component:
    type: kernel-modules
epoch: 1
`
		componentYaml = `component: some-snap+other-component
type: standard
version: 1.0
`
	)

	seq := snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{
		RealName: snapName,
		SnapID:   snapID,
		Revision: snap.R(7),
	}})

	snapstate.Set(s.state, snapName, &snapstate.SnapState{
		Active:   true,
		Sequence: seq,
		Current:  snap.R(7),
		SnapType: "app",
	})

	si := &snap.SideInfo{
		RealName: snapName,
		SnapID:   snapID,
		Revision: snap.R(9),
	}
	snapPath := makeTestSnap(c, snapYaml)

	csi := snap.ComponentSideInfo{
		Component: naming.NewComponentRef(snapName, compName),
		Revision:  snap.R(2),
	}

	components := []snapstate.PathComponent{{
		SideInfo: &csi,
		Path:     snaptest.MakeTestComponent(c, componentYaml),
	}}

	goal := snapstate.PathUpdateGoal(snapstate.PathSnap{
		InstanceName: snapName,
		Path:         snapPath,
		SideInfo:     si,
		Components:   components,
	})

	_, err := snapstate.UpdateOne(context.Background(), s.state, goal, nil, snapstate.Options{})
	c.Assert(err, ErrorMatches, fmt.Sprintf(`.*"%s" is not a component for snap "%s"`, compName, snapName))
}

func (s *targetTestSuite) TestInstallWithIntegrityDataEssentialSnap(c *C) {
	// Store has integrity data available and are being used for essential snaps
	s.state.Lock()
	defer s.state.Unlock()

	tests := []struct {
		instanceName string
		Comment      string
	}{
		{"some-base", "integrity data should be used for base snaps"},
		{"some-gadget", "integrity data should be used for gadget snaps"},
		{"some-kernel", "integrity data should be used for kernel snaps"},
		{"some-snapd", "integrity data should be used for the snapd snap"},
	}

	for _, tc := range tests {
		goal := snapstate.StoreInstallGoal(snapstate.StoreSnap{
			InstanceName: tc.instanceName,
			RevOpts: snapstate.RevisionOptions{
				Channel: "channel-with-integrity-data",
			},
		})

		_, ts, err := snapstate.InstallOne(context.Background(), s.state, goal, snapstate.Options{})
		c.Assert(err, IsNil)

		snapsup, err := snapstate.TaskSnapSetup(ts.Tasks()[0])
		c.Assert(err, IsNil)

		c.Check(snapsup.IntegrityDataInfo, Not(IsNil), Commentf(tc.Comment))
	}
}

func (s *targetTestSuite) TestInstallWithIntegrityDataApplicationSnap(c *C) {
	// Store has integrity data available but are not being used for application snaps
	s.state.Lock()
	defer s.state.Unlock()

	goal := snapstate.StoreInstallGoal(snapstate.StoreSnap{
		InstanceName: "some-snap",
		RevOpts: snapstate.RevisionOptions{
			Channel: "channel-with-integrity-data",
		},
	})

	_, ts, err := snapstate.InstallOne(context.Background(), s.state, goal, snapstate.Options{})
	c.Assert(err, IsNil)

	snapsup, err := snapstate.TaskSnapSetup(ts.Tasks()[0])
	c.Assert(err, IsNil)

	c.Check(snapsup.IntegrityDataInfo, IsNil)
}
