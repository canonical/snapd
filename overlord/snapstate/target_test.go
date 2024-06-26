package snapstate_test

import (
	"context"
	"fmt"

	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/snap"
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
