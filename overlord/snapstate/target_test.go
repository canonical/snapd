package snapstate_test

import (
	"context"
	"fmt"

	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/store"
	. "gopkg.in/check.v1"
)

type TargetTestSuite struct {
	snapmgrBaseTest
}

var _ = Suite(&TargetTestSuite{})

func (s *TargetTestSuite) TestInstallWithComponents(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	const (
		snapName = "some-snap"
		compName = "test-component"
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
				Type:      fmt.Sprintf("component/%s", snap.TestComponent),
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

func (s *TargetTestSuite) TestInstallWithComponentsMissingResource(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	const (
		snapName = "some-snap"
		compName = "test-component"
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
				Type:      fmt.Sprintf("component/%s", snap.TestComponent),
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

func (s *TargetTestSuite) TestInstallWithComponentsWrongType(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	const (
		snapName = "some-snap"
		compName = "test-component"
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
		`.*inconsistent component type \("%s" in snap, "%s" in component\)`, snap.TestComponent, snap.KernelModulesComponent,
	))
}

func (s *TargetTestSuite) TestInstallWithComponentsOtherResource(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	const (
		snapName = "test-snap"
		compName = "test-component"
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

func (s *TargetTestSuite) TestInstallWithComponentsMissingInInfo(c *C) {
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
				Type:      fmt.Sprintf("component/%s", snap.TestComponent),
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

func (s *TargetTestSuite) TestInstallWithComponentsFromPath(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	const (
		snapName = "some-snap"
		snapID   = "some-snap-id"
		compName = "test-component"
		snapYaml = `name: some-snap
version: 1.0
components:
  test-component:
    type: test
  kernel-modules-component:
    type: kernel-modules
`
		componentYaml = `component: some-snap+test-component
type: test
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

	csi := &snap.ComponentSideInfo{
		Component: naming.NewComponentRef(snapName, compName),
		Revision:  snap.R(3),
	}
	components := map[*snap.ComponentSideInfo]string{
		csi: snaptest.MakeTestComponent(c, componentYaml),
	}

	goal := snapstate.PathInstallGoal(snapName, snapPath, si, components, snapstate.RevisionOptions{})

	info, ts, err := snapstate.InstallOne(context.Background(), s.state, goal, snapstate.Options{})
	c.Assert(err, IsNil)

	c.Check(info.InstanceName(), Equals, snapName)
	c.Check(info.Components[compName].Name, Equals, compName)

	verifyInstallTasksWithComponents(c, snap.TypeApp, localSnap, 0, []string{compName}, ts)
}

func (s *TargetTestSuite) TestUpdateSnapNotInstalled(c *C) {
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

func (s *TargetTestSuite) TestInvalidPathGoals(c *C) {
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

		install := snapstate.PathInstallGoal(t.snap.InstanceName, t.snap.Path, t.snap.SideInfo, nil, t.snap.RevOpts)
		_, _, err = snapstate.InstallOne(context.Background(), s.state, install, snapstate.Options{})
		c.Check(err, ErrorMatches, t.err)
	}
}

func (s *TargetTestSuite) TestInstallFromStoreDefaultChannel(c *C) {
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

func (s *TargetTestSuite) TestInstallFromPathDefaultChannel(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapPath := makeTestSnap(c, `name: some-snap
version: 1.0
components:
  test-component:
    type: test
`)
	si := &snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(1),
	}

	goal := snapstate.PathInstallGoal(si.RealName, snapPath, si, nil, snapstate.RevisionOptions{})

	info, ts, err := snapstate.InstallOne(context.Background(), s.state, goal, snapstate.Options{})
	c.Assert(err, IsNil)

	c.Check(info.InstanceName(), Equals, "some-snap")
	c.Check(info.Channel, Equals, "")

	snapsup, err := snapstate.TaskSnapSetup(ts.Tasks()[0])
	c.Assert(err, IsNil)
	c.Check(snapsup.Channel, Equals, "")
}

func (s *TargetTestSuite) TestInstallFromPathSideInfoChannel(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapPath := makeTestSnap(c, `name: some-snap
version: 1.0
components:
  test-component:
    type: test
`)
	si := &snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(1),
		Channel:  "edge",
	}

	goal := snapstate.PathInstallGoal(si.RealName, snapPath, si, nil, snapstate.RevisionOptions{})

	info, ts, err := snapstate.InstallOne(context.Background(), s.state, goal, snapstate.Options{})
	c.Assert(err, IsNil)

	c.Check(info.InstanceName(), Equals, "some-snap")
	c.Check(info.Channel, Equals, "edge")

	snapsup, err := snapstate.TaskSnapSetup(ts.Tasks()[0])
	c.Assert(err, IsNil)
	c.Check(snapsup.Channel, Equals, "edge")
}

func (s *TargetTestSuite) TestInstallFromPathRevOptsChannel(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapPath := makeTestSnap(c, `name: some-snap
version: 1.0
components:
  test-component:
    type: test
`)
	si := &snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(1),
	}

	goal := snapstate.PathInstallGoal(si.RealName, snapPath, si, nil, snapstate.RevisionOptions{
		Channel: "edge",
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

func (s *TargetTestSuite) TestInstallFromPathRevOptsSideInfoChannelMismatch(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapPath := makeTestSnap(c, `name: some-snap
version: 1.0
components:
  test-component:
    type: test
`)
	si := &snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(1),
		Channel:  "stable",
	}

	goal := snapstate.PathInstallGoal(si.RealName, snapPath, si, nil, snapstate.RevisionOptions{
		Channel: "edge",
	})

	_, _, err := snapstate.InstallOne(context.Background(), s.state, goal, snapstate.Options{})
	c.Assert(err, ErrorMatches, `cannot install local snap "some-snap": edge != stable \(channel mismatch\)`)
}

func (s *TargetTestSuite) TestInstallFromStoreRevisionAndChannel(c *C) {
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

func (s *TargetTestSuite) TestInstallFromStoreRevisionAndChannelWithRedirectChannel(c *C) {
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
