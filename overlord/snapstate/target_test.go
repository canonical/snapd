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

func (s *targetTestSuite) TestInstallWithComponentsMissingResource(c *C) {
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

func (s *targetTestSuite) TestInstallWithComponentsWrongType(c *C) {
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
		`.*inconsistent component type \("component/%s" in snap, "component/%s" in component\)`, snap.TestComponent, snap.KernelModulesComponent,
	))
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

func (s *targetTestSuite) TestInstallWithComponentsFromPath(c *C) {
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

		install := snapstate.PathInstallGoal(t.snap.InstanceName, t.snap.Path, t.snap.SideInfo, nil, t.snap.RevOpts)
		_, _, err = snapstate.InstallOne(context.Background(), s.state, install, snapstate.Options{})
		c.Check(err, ErrorMatches, t.err)
	}
}

func (s *targetTestSuite) TestInstallComponentsFromPathInvalidComponentFile(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// use the real thing for this one
	snapstate.MockOpenSnapFile(backend.OpenSnapFile)

	const (
		snapID        = "some-snap-id"
		snapName      = "some-snap"
		componentName = "test-component"
	)
	snapRevision := snap.R(11)

	csi := snap.ComponentSideInfo{
		Component: naming.NewComponentRef(snapName, componentName),
		Revision:  snap.R(1),
	}

	compPath := filepath.Join(c.MkDir(), "invalid-component")
	err := os.WriteFile(compPath, []byte("invalid-component"), 0644)
	c.Assert(err, IsNil)

	components := map[*snap.ComponentSideInfo]string{
		&csi: compPath,
	}

	snapPath := makeTestSnap(c, `name: some-snap
version: 1.0
components:
  test-component:
    type: test
`)
	si := &snap.SideInfo{
		RealName: snapName,
		SnapID:   snapID,
		Revision: snapRevision,
	}

	goal := snapstate.PathInstallGoal(snapName, snapPath, si, components, snapstate.RevisionOptions{})
	_, _, err = snapstate.InstallOne(context.Background(), s.state, goal, snapstate.Options{})
	c.Assert(err, ErrorMatches, fmt.Sprintf(`.*cannot process snap or snapdir: file "%s" is invalid.*`, compPath))
}

func (s *targetTestSuite) TestInstallComponentsFromPathInvalidComponentName(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// use the real thing for this one
	snapstate.MockOpenSnapFile(backend.OpenSnapFile)

	const (
		snapID        = "some-snap-id"
		snapName      = "some-snap"
		componentName = "Bad-component"
	)
	snapRevision := snap.R(11)

	csi := snap.ComponentSideInfo{
		Component: naming.NewComponentRef(snapName, componentName),
		Revision:  snap.R(1),
	}

	components := map[*snap.ComponentSideInfo]string{
		&csi: "",
	}

	snapPath := makeTestSnap(c, `name: some-snap
version: 1.0
components:
  test-component:
    type: test
`)
	si := &snap.SideInfo{
		RealName: snapName,
		SnapID:   snapID,
		Revision: snapRevision,
	}

	goal := snapstate.PathInstallGoal(snapName, snapPath, si, components, snapstate.RevisionOptions{})
	_, _, err := snapstate.InstallOne(context.Background(), s.state, goal, snapstate.Options{})
	c.Assert(err, ErrorMatches, fmt.Sprintf(`invalid snap name: "%s"`, componentName))
}

func (s *targetTestSuite) TestUpdateComponents(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	const (
		snapName = "some-snap"
		snapID   = "some-snap-id"
		compName = "test-component"
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
		CompType: snap.TestComponent,
	})

	s.AddCleanup(snapstate.MockReadComponentInfo(func(
		compMntDir string, info *snap.Info, csi *snap.ComponentSideInfo,
	) (*snap.ComponentInfo, error) {
		return &snap.ComponentInfo{
			Component:         naming.NewComponentRef(info.SnapName(), compName),
			Type:              snap.TestComponent,
			Version:           "1.0",
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
				Type:      fmt.Sprintf("component/%s", snap.TestComponent),
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

	verifyUpdateTasksWithComponents(c, snap.TypeApp, doesReRefresh, 0, []string{compName}, ts)
}

func (s *targetTestSuite) TestUpdateComponentsFromPath(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	const (
		snapName = "some-snap"
		snapID   = "some-snap-id"
		compName = "test-component"
		channel  = "channel-for-components"
		snapYaml = `name: some-snap
version: 1.0
components:
  test-component:
    type: test
  kernel-modules-component:
    type: kernel-modules
epoch: 1
`
		componentYaml = `component: some-snap+test-component
type: test
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
		CompType: snap.TestComponent,
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

	csi := &snap.ComponentSideInfo{
		Component: naming.NewComponentRef(snapName, compName),
		Revision:  snap.R(2),
	}
	components := map[*snap.ComponentSideInfo]string{
		csi: snaptest.MakeTestComponent(c, componentYaml),
	}

	goal := snapstate.PathUpdateGoal(snapstate.PathSnap{
		InstanceName: snapName,
		Path:         snapPath,
		SideInfo:     si,
		Components:   components,
	})

	ts, err := snapstate.UpdateOne(context.Background(), s.state, goal, nil, snapstate.Options{})
	c.Assert(err, IsNil)

	verifyUpdateTasksWithComponents(c, snap.TypeApp, doesReRefresh|localSnap, 0, []string{compName}, ts)
}

func (s *targetTestSuite) TestUpdateComponentsFromPathInvalidComponentFile(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	const (
		snapName = "some-snap"
		snapID   = "some-snap-id"
		compName = "test-component"
		channel  = "channel-for-components"
		snapYaml = `name: some-snap
version: 1.0
components:
  test-component:
    type: test
  kernel-modules-component:
    type: kernel-modules
epoch: 1
`
		componentYaml = `component: some-snap+test-component
type: test
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
		CompType: snap.TestComponent,
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

	components := map[*snap.ComponentSideInfo]string{
		&csi: compPath,
	}

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
		compName = "test-component"
		snapYaml = `name: some-snap
version: 1.0
components:
  test-component:
    type: test
  kernel-modules-component:
    type: kernel-modules
epoch: 1
`
		componentYaml = `component: some-snap+test-component
type: test
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
		CompType: snap.TestComponent,
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

	components := map[*snap.ComponentSideInfo]string{
		&csi: compPath,
	}

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
version: 1.0
components:
  test-component:
    type: test
  kernel-modules-component:
    type: kernel-modules
epoch: 1
`
		componentYaml = `component: some-snap+other-component
type: test
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

	components := map[*snap.ComponentSideInfo]string{
		&csi: snaptest.MakeTestComponent(c, componentYaml),
	}

	goal := snapstate.PathUpdateGoal(snapstate.PathSnap{
		InstanceName: snapName,
		Path:         snapPath,
		SideInfo:     si,
		Components:   components,
	})

	_, err := snapstate.UpdateOne(context.Background(), s.state, goal, nil, snapstate.Options{})
	c.Assert(err, ErrorMatches, fmt.Sprintf(`.*"%s" is not a component for snap "%s"`, compName, snapName))
}
