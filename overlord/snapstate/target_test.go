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
}

func (s *TargetTestSuite) TestInstallWithComponentsMissingResource(c *C) {
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
		`.*inconsistent component type \("component/%s" in snap, "component/%s" in component\)`, snap.TestComponent, snap.KernelModulesComponent,
	))
}

func (s *TargetTestSuite) TestInstallWithComponentsMissingInInfo(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	const (
		snapName = "test-snap"
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
		snapName = "test-snap"
		snapID   = "test-snap-id"
		compName = "test-component"
		snapYaml = `name: test-snap
version: 1.0
components:
  test-component:
    type: test
  kernel-modules-component:
    type: kernel-modules
`
		componentYaml = `component: test-snap+test-component
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
