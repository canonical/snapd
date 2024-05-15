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
					DownloadURL: "http://example.com/mycomp",
				},
				Name:      compName,
				Revision:  1,
				Type:      fmt.Sprintf("component/%s", snap.TestComponent),
				Version:   "1.0",
				CreatedAt: "2024-01-01T00:00:00Z",
			},
		}
	}

	goal := snapstate.StoreGoal(snapstate.StoreSnap{
		InstanceName: "test-snap",
		Components:   []string{"test-component"},
		RevOpts: snapstate.RevisionOptions{
			Channel: channel,
		},
	})

	infos, tss, err := snapstate.InstallWithGoal(context.Background(), s.state, goal, snapstate.Options{})
	c.Assert(err, IsNil)
	c.Assert(infos, HasLen, 1)
	c.Assert(tss, HasLen, 1)

	info := infos[0]
	c.Check(info.InstanceName(), Equals, snapName)
	c.Check(info.Channel, Equals, channel)
	c.Check(info.Components[compName].Name, Equals, compName)

	ts := tss[0]

	verifyInstallTasksWithComponents(c, snap.TypeApp, 0, 0, []string{compName}, ts)
}
