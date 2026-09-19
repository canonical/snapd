package snapstate_test

import (
	. "gopkg.in/check.v1"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
)

type reinstallSuite struct {
	state *state.State
}

var _ = Suite(&reinstallSuite{})

func (s *reinstallSuite) SetUpTest(c *C) {
	s.state = state.New(nil)
	s.state.Lock()
	defer s.state.Unlock()
	snapstate.Set(s.state, "test-app", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snapstate.SnapRevision{{Revision: snapstate.Revision{N: 1}}},
		Current:  snapstate.Revision{N: 1},
	})
}

func (s *reinstallSuite) TestReinstallTaskChain(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	opts := &snapstate.ReinstallOptions{
		Purge: true,
	}

	ts, err := snapstate.Reinstall(s.state, "test-app", opts)
	c.Assert(err, IsNil)
	c.Assert(ts, NotNil)

	tasks := ts.Tasks()
	
	var hasRemoveSnap, hasDownloadSnap bool
	var removeSnapTask, downloadSnapTask *state.Task

	for _, t := range tasks {
		switch t.Kind() {
		case "remove-snap":
			hasRemoveSnap = true
			removeSnapTask = t
		case "download-snap":
			hasDownloadSnap = true
			downloadSnapTask = t
		}
	}

	c.Assert(hasRemoveSnap, Equals, true)
	c.Assert(hasDownloadSnap, Equals, true)

	waitList := downloadSnapTask.WaitTasks()
	
	foundDependency := false
	for _, w := range waitList {
		if w == removeSnapTask {
			foundDependency = true
			break
		}
	}
	
	c.Assert(foundDependency, Equals, true, Commentf("download-snap is not waiting for remove-snap to finish"))
}
