// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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

package ctlcmd_test

import (
	"errors"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/hookstate/ctlcmd"
	"github.com/snapcore/snapd/overlord/hookstate/hooktest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
)

type ResultForEnsureMountUnitFileWithOptions struct {
	path string
	err  error
}

type FakeSystemdForMount struct {
	systemd.Systemd

	RemoveMountUnitFileCalls  []string
	RemoveMountUnitFileResult error

	EnsureMountUnitFileWithOptionsCalls  []*systemd.MountUnitOptions
	EnsureMountUnitFileWithOptionsResult ResultForEnsureMountUnitFileWithOptions
}

func (s *FakeSystemdForMount) RemoveMountUnitFile(baseDir string) error {
	s.RemoveMountUnitFileCalls = append(s.RemoveMountUnitFileCalls, baseDir)
	return s.RemoveMountUnitFileResult
}

func (s *FakeSystemdForMount) EnsureMountUnitFileWithOptions(options *systemd.MountUnitOptions) (string, error) {
	s.EnsureMountUnitFileWithOptionsCalls = append(s.EnsureMountUnitFileWithOptionsCalls, options)
	return s.EnsureMountUnitFileWithOptionsResult.path, s.EnsureMountUnitFileWithOptionsResult.err
}

func CopyMap(m map[string]any) map[string]any {
	cp := make(map[string]any)
	for k, v := range m {
		switch value := v.(type) {
		case map[string]any:
			cp[k] = CopyMap(value)
		case []any:
			cp[k] = CopySlice(value)
		default:
			cp[k] = v
		}
	}
	return cp
}

func CopySlice(s []any) []any {
	cp := make([]any, len(s))
	for i, v := range s {
		switch value := v.(type) {
		case map[string]any:
			cp[i] = CopyMap(value)
		case []any:
			cp[i] = CopySlice(value)
		default:
			cp[i] = v
		}
	}
	return cp
}

type mountSuite struct {
	testutil.BaseTest
	state       *state.State
	mockContext *hookstate.Context
	mockHandler *hooktest.MockHandler
	hookTask    *state.Task
	sysd        *FakeSystemdForMount
	// A connection state for a snap using the mount interface with the plug
	// properly configured, which we'll be reusing in different test cases
	regularConnState map[string]any
}

var _ = Suite(&mountSuite{})

func (s *mountSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	dirs.SetRootDir(c.MkDir())

	s.mockHandler = hooktest.NewMockHandler()

	s.state = state.New(nil)
	s.state.Lock()
	defer s.state.Unlock()
	task := s.state.NewTask("test-task", "my test task")
	setup := &hookstate.HookSetup{Snap: "snap1", Revision: snap.R(42), Hook: "mount"}

	ctx, err := hookstate.NewContext(task, s.state, setup, s.mockHandler, "")
	c.Assert(err, IsNil)
	s.mockContext = ctx

	s.regularConnState = map[string]any{
		"interface": "mount-control",
		"plug-static": map[string]any{
			"mount": []any{
				map[string]any{
					"what":       "/src",
					"where":      "/dest",
					"type":       []string{"ext4"},
					"options":    []string{"bind", "rw", "sync"},
					"persistent": true,
				},
				map[string]any{
					"what":       "/media/me/data",
					"where":      "$SNAP_DATA/dest",
					"options":    []string{"bind", "ro"},
					"persistent": false,
				},
				map[string]any{
					"what":       "/dev/dma_heap/qcom,qseecom",
					"where":      "/dest,with,commas",
					"options":    []string{"ro"},
					"persistent": false,
				},
				map[string]any{
					"where":   "/nfs-dest",
					"options": []string{"rw"},
					"type":    []string{"nfs"},
				},
				map[string]any{
					"where":   "/cifs-dest",
					"options": []string{"rw", "guest"},
					"type":    []string{"cifs"},
				},
			},
		},
	}
	s.hookTask = task

	s.sysd = &FakeSystemdForMount{}
	s.AddCleanup(systemd.MockNewSystemd(func(be systemd.Backend, roodDir string, mode systemd.InstanceMode, meter systemd.Reporter) systemd.Systemd {
		return s.sysd
	}))
}

func (s *mountSuite) injectSnapWithProperPlug(c *C) {
	s.state.Lock()
	mockInstalledSnap(c, s.state, `name: snap1`, "")
	s.state.Set("conns", map[string]any{
		"snap1:plug1 snap2:slot2": s.regularConnState,
	})
	s.state.Unlock()
}

func (s *mountSuite) TestMissingContext(c *C) {
	_, _, err := ctlcmd.Run(nil, []string{"mount", "/src", "/dest"}, 0)
	c.Check(err, ErrorMatches, `cannot invoke snapctl operation commands \(here "mount"\) from outside of a snap`)
}

func (s *mountSuite) TestBadConnection(c *C) {
	setup := &hookstate.HookSetup{}

	// Inject some invalid connection data into the state, so that
	// ifacestate.ConnectionStates() will return an error.
	state := state.New(nil)
	state.Lock()
	task := state.NewTask("test-task", "my test task")
	state.Set("conns", "I wish I was JSON")
	state.Unlock()
	ctx, err := hookstate.NewContext(task, state, setup, s.mockHandler, "")
	c.Assert(err, IsNil)

	_, _, err = ctlcmd.Run(ctx, []string{"mount", "/src", "/dest"}, 0)
	c.Assert(err, ErrorMatches, `.*internal error: cannot get connections: .*`)
}

func (s *mountSuite) TestBadSnapInfo(c *C) {
	s.state.Lock()
	task := s.state.NewTask("test-task", "my test task")
	setup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(42), Hook: "configure"}
	s.state.Unlock()

	ctx, err := hookstate.NewContext(task, s.state, setup, s.mockHandler, "")
	c.Assert(err, IsNil)

	_, _, err = ctlcmd.Run(ctx, []string{"mount", "/src", "/dest"}, 0)
	c.Assert(err, ErrorMatches, `.*cannot get snap info: snap \"test-snap\" is not installed`)
}

func (s *mountSuite) TestMissingProperPlug(c *C) {
	s.state.Lock()
	mockInstalledSnap(c, s.state, `name: snap1`, "")
	// Inject a lot of connections in the state, but all of them defective for
	// one or another reason
	connections := make(map[string]any)
	// wrong interface
	conn := CopyMap(s.regularConnState)
	conn["interface"] = "unrelated"
	connections["snap1:plug1 snap2:slot1"] = conn
	// undesired
	conn = CopyMap(s.regularConnState)
	conn["undesired"] = true
	connections["snap1:plug2 snap2:slot1"] = conn
	// hotplug gone
	conn = CopyMap(s.regularConnState)
	conn["hotplug-gone"] = true
	connections["snap1:plug3 snap2:slot1"] = conn
	// different snap
	conn = CopyMap(s.regularConnState)
	connections["othersnap:plug1 snap2:slot1"] = conn
	// missing plug info
	conn = CopyMap(s.regularConnState)
	delete(conn, "plug-static")
	connections["snap1:plug4 snap2:slot1"] = conn
	// incompatible "what" field
	conn = CopyMap(s.regularConnState)
	plugInfo := func(conn map[string]any) map[string]any {
		return conn["plug-static"].(map[string]any)["mount"].([]any)[0].(map[string]any)
	}
	plugInfo(conn)["what"] = "/some/other/path"
	connections["snap1:plug5 snap2:slot1"] = conn
	// wrong type for "what" field
	conn = CopyMap(s.regularConnState)
	plugInfo(conn)["what"] = []string{}
	connections["snap1:plug6 snap2:slot1"] = conn
	// incompatible "where" field
	conn = CopyMap(s.regularConnState)
	plugInfo(conn)["where"] = "/some/other/target/path"
	connections["snap1:plug7 snap2:slot1"] = conn
	// incompatible "type" field
	conn = CopyMap(s.regularConnState)
	plugInfo(conn)["type"] = []string{"vfat"}
	connections["snap1:plug8 snap2:slot1"] = conn
	// wrong type for "type" field
	conn = CopyMap(s.regularConnState)
	plugInfo(conn)["type"] = "ext4"
	connections["snap1:plug9 snap2:slot1"] = conn
	// incompatible "options" field
	conn = CopyMap(s.regularConnState)
	plugInfo(conn)["options"] = []string{"rw"}
	connections["snap1:plug10 snap2:slot1"] = conn
	// no persistent mounts allowed
	conn = CopyMap(s.regularConnState)
	plugInfo(conn)["persistent"] = false
	connections["snap1:plug11 snap2:slot1"] = conn
	// like above, just not expliticly
	conn = CopyMap(s.regularConnState)
	delete(plugInfo(conn), "persistent")
	connections["snap1:plug12 snap2:slot1"] = conn

	s.state.Set("conns", connections)
	s.state.Unlock()

	_, _, err := ctlcmd.Run(s.mockContext, []string{"mount", "--persistent", "-t", "ext4", "-o", "bind,rw", "/src", "/dest"}, 0)
	c.Check(err, ErrorMatches, `.*no matching mount-control connection found`)
	c.Check(s.sysd.EnsureMountUnitFileWithOptionsCalls, HasLen, 0)

	// Try the same without the filesystem type
	_, _, err = ctlcmd.Run(s.mockContext, []string{"mount", "--persistent", "-o", "bind,rw", "/src", "/dest"}, 0)
	c.Check(err, ErrorMatches, `.*no matching mount-control connection found`)
	c.Check(s.sysd.EnsureMountUnitFileWithOptionsCalls, HasLen, 0)

	// bad NFS source format, expecting <host>:<share>
	_, _, err = ctlcmd.Run(s.mockContext, []string{"mount", "-o", "rw", "-t", "nfs", "/src", "/dest"}, 0)
	c.Check(err, ErrorMatches, `.*no matching mount-control connection found`)
	_, _, err = ctlcmd.Run(s.mockContext, []string{"mount", "-o", "rw", "-t", "nfs", "/host:/src", "/dest"}, 0)
	c.Check(err, ErrorMatches, `.*no matching mount-control connection found`)
	_, _, err = ctlcmd.Run(s.mockContext, []string{"mount", "-o", "rw", "-t", "nfs", ":/share", "/dest"}, 0)
	c.Check(err, ErrorMatches, `.*no matching mount-control connection found`)
	c.Check(s.sysd.EnsureMountUnitFileWithOptionsCalls, HasLen, 0)

	// bad CIFS source format, expecting //<host-share>
	_, _, err = ctlcmd.Run(s.mockContext, []string{"mount", "-o", "rw,guest", "-t", "cifs", "/src", "/dest"}, 0)
	c.Check(err, ErrorMatches, `.*no matching mount-control connection found`)
	_, _, err = ctlcmd.Run(s.mockContext, []string{"mount", "-o", "rw,guest", "-t", "cifs", "host:/src", "/dest"}, 0)
	c.Check(err, ErrorMatches, `.*no matching mount-control connection found`)
	_, _, err = ctlcmd.Run(s.mockContext, []string{"mount", "-o", "rw,guest", "-t", "cifs", "/share", "/dest"}, 0)
	c.Check(err, ErrorMatches, `.*no matching mount-control connection found`)
	_, _, err = ctlcmd.Run(s.mockContext, []string{"mount", "-o", "rw,guest", "-t", "cifs", "//", "/dest"}, 0)
	c.Check(err, ErrorMatches, `.*no matching mount-control connection found`)
	// incorrect CIFS mount options
	_, _, err = ctlcmd.Run(s.mockContext, []string{"mount", "-o", "rw", "-t", "cifs", "//foo/share", "/dest"}, 0)
	c.Check(err, ErrorMatches, `.*no matching mount-control connection found`)
	c.Check(s.sysd.EnsureMountUnitFileWithOptionsCalls, HasLen, 0)
}

func (s *mountSuite) TestUnitCreationFailure(c *C) {
	s.injectSnapWithProperPlug(c)

	s.sysd.EnsureMountUnitFileWithOptionsResult = ResultForEnsureMountUnitFileWithOptions{"", errors.New("creation error")}

	_, _, err := ctlcmd.Run(s.mockContext, []string{"mount", "-t", "ext4", "/src", "/dest"}, 0)
	c.Check(err, ErrorMatches, `cannot ensure mount unit: creation error`)
	c.Check(s.sysd.EnsureMountUnitFileWithOptionsCalls, DeepEquals, []*systemd.MountUnitOptions{
		{
			Lifetime:               systemd.Transient,
			Description:            "Mount unit for snap1, revision 1 via mount-control",
			What:                   "/src",
			Where:                  "/dest",
			Fstype:                 "ext4",
			Origin:                 "mount-control",
			EnsureStartIfUnchanged: true,
		},
	})
}

func (s *mountSuite) TestHappy(c *C) {
	s.injectSnapWithProperPlug(c)

	s.sysd.EnsureMountUnitFileWithOptionsResult = ResultForEnsureMountUnitFileWithOptions{"/path/unit.mount", nil}

	_, _, err := ctlcmd.Run(s.mockContext, []string{"mount", "--persistent", "-t", "ext4", "-o", "sync,rw", "/src", "/dest"}, 0)
	c.Check(err, IsNil)
	c.Check(s.sysd.EnsureMountUnitFileWithOptionsCalls, DeepEquals, []*systemd.MountUnitOptions{
		{
			Lifetime:               systemd.Persistent,
			Description:            "Mount unit for snap1, revision 1 via mount-control",
			What:                   "/src",
			Where:                  "/dest",
			Fstype:                 "ext4",
			Options:                []string{"sync", "rw"},
			Origin:                 "mount-control",
			EnsureStartIfUnchanged: true,
		},
	})
}

func (s *mountSuite) TestHappyWithVariableExpansion(c *C) {
	s.injectSnapWithProperPlug(c)

	s.sysd.EnsureMountUnitFileWithOptionsResult = ResultForEnsureMountUnitFileWithOptions{"/path/unit.mount", nil}

	// Now try with $SNAP_* variables in the paths
	snapDataDir := filepath.Join(dirs.SnapDataDir, "snap1", "1")
	where := filepath.Join(snapDataDir, "/dest")
	_, _, err := ctlcmd.Run(s.mockContext, []string{"mount", "-o", "bind,ro", "/media/me/data", where}, 0)
	c.Check(err, IsNil)
	c.Check(s.sysd.EnsureMountUnitFileWithOptionsCalls, DeepEquals, []*systemd.MountUnitOptions{
		{
			Lifetime:               systemd.Transient,
			Description:            "Mount unit for snap1, revision 1 via mount-control",
			What:                   "/media/me/data",
			Where:                  where,
			Options:                []string{"bind", "ro"},
			Origin:                 "mount-control",
			EnsureStartIfUnchanged: true,
		},
	})
}

func (s *mountSuite) TestHappyWithCommasInPath(c *C) {
	s.injectSnapWithProperPlug(c)

	s.sysd.EnsureMountUnitFileWithOptionsResult = ResultForEnsureMountUnitFileWithOptions{"/path/unit.mount", nil}

	// Now try with commas in the paths
	_, _, err := ctlcmd.Run(s.mockContext, []string{"mount", "-o", "ro", "/dev/dma_heap/qcom,qseecom", "/dest,with,commas"}, 0)
	c.Check(err, IsNil)
	c.Check(s.sysd.EnsureMountUnitFileWithOptionsCalls, DeepEquals, []*systemd.MountUnitOptions{
		{
			Lifetime:               systemd.Transient,
			Description:            "Mount unit for snap1, revision 1 via mount-control",
			What:                   "/dev/dma_heap/qcom,qseecom",
			Where:                  "/dest,with,commas",
			Options:                []string{"ro"},
			Origin:                 "mount-control",
			EnsureStartIfUnchanged: true,
		},
	})
}

func (s *mountSuite) TestHappyNFS(c *C) {
	s.injectSnapWithProperPlug(c)

	s.sysd.EnsureMountUnitFileWithOptionsResult = ResultForEnsureMountUnitFileWithOptions{"/path/unit.mount", nil}

	// Now try with commas in the paths
	_, _, err := ctlcmd.Run(s.mockContext, []string{"mount", "-o", "rw", "-t", "nfs", "localhost:/var/share", "/nfs-dest"}, 0)
	c.Check(err, IsNil)
	c.Check(s.sysd.EnsureMountUnitFileWithOptionsCalls, DeepEquals, []*systemd.MountUnitOptions{
		{
			Lifetime:               systemd.Transient,
			Description:            "Mount unit for snap1, revision 1 via mount-control",
			What:                   "localhost:/var/share",
			Where:                  "/nfs-dest",
			Fstype:                 "nfs",
			Options:                []string{"rw"},
			Origin:                 "mount-control",
			EnsureStartIfUnchanged: true,
		},
	})
}

func (s *mountSuite) TestHappyCIFS(c *C) {
	s.injectSnapWithProperPlug(c)

	s.sysd.EnsureMountUnitFileWithOptionsResult = ResultForEnsureMountUnitFileWithOptions{"/path/unit.mount", nil}

	// Now try with commas in the paths
	_, _, err := ctlcmd.Run(s.mockContext, []string{"mount", "-o", "rw,guest", "-t", "cifs", "//10.0.0.1/share/path", "/cifs-dest"}, 0)
	c.Check(err, IsNil)
	c.Check(s.sysd.EnsureMountUnitFileWithOptionsCalls, DeepEquals, []*systemd.MountUnitOptions{
		{
			Lifetime:               systemd.Transient,
			Description:            "Mount unit for snap1, revision 1 via mount-control",
			What:                   "//10.0.0.1/share/path",
			Where:                  "/cifs-dest",
			Fstype:                 "cifs",
			Options:                []string{"rw", "guest"},
			Origin:                 "mount-control",
			EnsureStartIfUnchanged: true,
		},
	})
}

func (s *mountSuite) TestEnsureMountUnitFailed(c *C) {
	s.injectSnapWithProperPlug(c)

	s.sysd.EnsureMountUnitFileWithOptionsResult = ResultForEnsureMountUnitFileWithOptions{"", errors.New("some error")}

	_, _, err := ctlcmd.Run(s.mockContext, []string{"mount", "--persistent", "-t", "ext4", "-o", "sync,rw", "/src", "/dest"}, 0)
	c.Check(err, ErrorMatches, `cannot ensure mount unit: some error`)
	c.Check(s.sysd.EnsureMountUnitFileWithOptionsCalls, DeepEquals, []*systemd.MountUnitOptions{
		{
			Lifetime:               systemd.Persistent,
			Description:            "Mount unit for snap1, revision 1 via mount-control",
			What:                   "/src",
			Where:                  "/dest",
			Fstype:                 "ext4",
			Options:                []string{"sync", "rw"},
			Origin:                 "mount-control",
			EnsureStartIfUnchanged: true,
		},
	})

	c.Check(s.sysd.RemoveMountUnitFileCalls, DeepEquals, []string{"/dest"})
}

func (s *mountSuite) TestEnsureMountUnitFailedRemoveFailed(c *C) {
	s.injectSnapWithProperPlug(c)

	s.sysd.EnsureMountUnitFileWithOptionsResult = ResultForEnsureMountUnitFileWithOptions{"", errors.New("some error")}
	s.sysd.RemoveMountUnitFileResult = errors.New("some other error")

	_, _, err := ctlcmd.Run(s.mockContext, []string{"mount", "--persistent", "-t", "ext4", "-o", "sync,rw", "/src", "/dest"}, 0)
	c.Check(err, ErrorMatches, `cannot ensure mount unit: some error`)
	c.Check(s.sysd.EnsureMountUnitFileWithOptionsCalls, DeepEquals, []*systemd.MountUnitOptions{
		{
			Lifetime:               systemd.Persistent,
			Description:            "Mount unit for snap1, revision 1 via mount-control",
			What:                   "/src",
			Where:                  "/dest",
			Fstype:                 "ext4",
			Options:                []string{"sync", "rw"},
			Origin:                 "mount-control",
			EnsureStartIfUnchanged: true,
		},
	})

	c.Check(s.sysd.RemoveMountUnitFileCalls, DeepEquals, []string{"/dest"})
}
