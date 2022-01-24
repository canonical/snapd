// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2021 Canonical Ltd
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

package wrappers_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget/quantity"

	// imported to ensure actual interfaces are defined (in production this is guaranteed by ifacestate)
	_ "github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/quota"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/systemd/systemdtest"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timings"
	"github.com/snapcore/snapd/usersession/agent"
	"github.com/snapcore/snapd/wrappers"
)

type servicesTestSuite struct {
	testutil.DBusTest

	tempdir string

	sysdLog [][]string

	systemctlRestorer, delaysRestorer func()

	perfTimings timings.Measurer

	agent *agent.SessionAgent
}

var _ = Suite(&servicesTestSuite{})

func (s *servicesTestSuite) SetUpTest(c *C) {
	s.DBusTest.SetUpTest(c)
	s.tempdir = c.MkDir()
	s.sysdLog = nil
	dirs.SetRootDir(s.tempdir)

	s.systemctlRestorer = systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		s.sysdLog = append(s.sysdLog, cmd)
		return []byte("ActiveState=inactive\n"), nil
	})
	s.delaysRestorer = systemd.MockStopDelays(time.Millisecond, 25*time.Second)
	s.perfTimings = timings.New(nil)

	xdgRuntimeDir := fmt.Sprintf("%s/%d", dirs.XdgRuntimeDirBase, os.Getuid())
	err := os.MkdirAll(xdgRuntimeDir, 0700)
	c.Assert(err, IsNil)
	s.agent, err = agent.New()
	c.Assert(err, IsNil)
	s.agent.Start()
}

func (s *servicesTestSuite) TearDownTest(c *C) {
	if s.agent != nil {
		err := s.agent.Stop()
		c.Check(err, IsNil)
	}
	s.systemctlRestorer()
	s.delaysRestorer()
	dirs.SetRootDir("")
	s.DBusTest.TearDownTest(c)
}

func (s *servicesTestSuite) TestAddSnapServicesAndRemove(c *C) {
	info := snaptest.MockSnap(c, packageHello, &snap.SideInfo{Revision: snap.R(12)})
	svcFile := filepath.Join(s.tempdir, "/etc/systemd/system/snap.hello-snap.svc1.service")

	err := wrappers.AddSnapServices(info, nil, progress.Null)
	c.Assert(err, IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"daemon-reload"},
	})

	s.sysdLog = nil

	flags := &wrappers.StartServicesFlags{Enable: true}
	err = wrappers.StartServices(info.Services(), nil, flags, progress.Null, s.perfTimings)
	c.Assert(err, IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"enable", filepath.Base(svcFile)},
		{"start", filepath.Base(svcFile)},
	})

	dir := filepath.Join(dirs.SnapMountDir, "hello-snap", "12.mount")
	c.Assert(svcFile, testutil.FileEquals, fmt.Sprintf(`[Unit]
# Auto-generated, DO NOT EDIT
Description=Service for snap application hello-snap.svc1
Requires=%[1]s
Wants=network.target
After=%[1]s network.target snapd.apparmor.service
X-Snappy=yes

[Service]
EnvironmentFile=-/etc/environment
ExecStart=/usr/bin/snap run hello-snap.svc1
SyslogIdentifier=hello-snap.svc1
Restart=on-failure
WorkingDirectory=%[2]s/var/snap/hello-snap/12
ExecStop=/usr/bin/snap run --command=stop hello-snap.svc1
ExecStopPost=/usr/bin/snap run --command=post-stop hello-snap.svc1
TimeoutStopSec=30
Type=forking

[Install]
WantedBy=multi-user.target
`,
		systemd.EscapeUnitNamePath(dir),
		dirs.GlobalRootDir,
	))

	s.sysdLog = nil
	err = wrappers.StopServices(info.Services(), nil, "", progress.Null, s.perfTimings)
	c.Assert(err, IsNil)
	c.Assert(s.sysdLog, HasLen, 2)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"stop", filepath.Base(svcFile)},
		{"show", "--property=ActiveState", "snap.hello-snap.svc1.service"},
	})

	s.sysdLog = nil
	err = wrappers.RemoveSnapServices(info, progress.Null)
	c.Assert(err, IsNil)
	c.Check(svcFile, testutil.FileAbsent)
	c.Assert(s.sysdLog, DeepEquals, [][]string{
		{"disable", filepath.Base(svcFile)},
		{"daemon-reload"},
	})
}
func (s *servicesTestSuite) TestEnsureSnapServicesAdds(c *C) {
	// map unit -> new
	seen := make(map[string]bool)
	cb := func(app *snap.AppInfo, grp *quota.Group, unitType, name string, old, new string) {
		seen[fmt.Sprintf("%s:%s:%s:%s", app.Snap.InstanceName(), app.Name, unitType, name)] = old == ""
	}

	info := snaptest.MockSnap(c, packageHello, &snap.SideInfo{Revision: snap.R(12)})
	svcFile := filepath.Join(s.tempdir, "/etc/systemd/system/snap.hello-snap.svc1.service")

	m := map[*snap.Info]*wrappers.SnapServiceOptions{
		info: nil,
	}

	err := wrappers.EnsureSnapServices(m, nil, cb, progress.Null)
	c.Assert(err, IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"daemon-reload"},
	})
	c.Check(seen, DeepEquals, map[string]bool{
		"hello-snap:svc1:service:svc1": true,
	})

	dir := filepath.Join(dirs.SnapMountDir, "hello-snap", "12.mount")
	c.Assert(svcFile, testutil.FileEquals, fmt.Sprintf(`[Unit]
# Auto-generated, DO NOT EDIT
Description=Service for snap application hello-snap.svc1
Requires=%[1]s
Wants=network.target
After=%[1]s network.target snapd.apparmor.service
X-Snappy=yes

[Service]
EnvironmentFile=-/etc/environment
ExecStart=/usr/bin/snap run hello-snap.svc1
SyslogIdentifier=hello-snap.svc1
Restart=on-failure
WorkingDirectory=%[2]s/var/snap/hello-snap/12
ExecStop=/usr/bin/snap run --command=stop hello-snap.svc1
ExecStopPost=/usr/bin/snap run --command=post-stop hello-snap.svc1
TimeoutStopSec=30
Type=forking

[Install]
WantedBy=multi-user.target
`,
		systemd.EscapeUnitNamePath(dir),
		dirs.GlobalRootDir,
	))
}

func (s *servicesTestSuite) TestEnsureSnapServicesWithQuotas(c *C) {
	info := snaptest.MockSnap(c, packageHello, &snap.SideInfo{Revision: snap.R(12)})
	svcFile := filepath.Join(s.tempdir, "/etc/systemd/system/snap.hello-snap.svc1.service")

	memLimit := quantity.SizeGiB
	grp, err := quota.NewGroup("foogroup", memLimit)
	c.Assert(err, IsNil)

	m := map[*snap.Info]*wrappers.SnapServiceOptions{
		info: {QuotaGroup: grp},
	}

	dir := filepath.Join(dirs.SnapMountDir, "hello-snap", "12.mount")
	svcContent := fmt.Sprintf(`[Unit]
# Auto-generated, DO NOT EDIT
Description=Service for snap application hello-snap.svc1
Requires=%[1]s
Wants=network.target
After=%[1]s network.target snapd.apparmor.service
X-Snappy=yes

[Service]
EnvironmentFile=-/etc/environment
ExecStart=/usr/bin/snap run hello-snap.svc1
SyslogIdentifier=hello-snap.svc1
Restart=on-failure
WorkingDirectory=%[2]s/var/snap/hello-snap/12
ExecStop=/usr/bin/snap run --command=stop hello-snap.svc1
ExecStopPost=/usr/bin/snap run --command=post-stop hello-snap.svc1
TimeoutStopSec=30
Type=forking
Slice=snap.foogroup.slice

[Install]
WantedBy=multi-user.target
`,
		systemd.EscapeUnitNamePath(dir),
		dirs.GlobalRootDir,
	)

	sliceTempl := `[Unit]
Description=Slice for snap quota group %s
Before=slices.target
X-Snappy=yes

[Slice]
# Always enable memory accounting otherwise the MemoryMax setting does nothing.
MemoryAccounting=true
MemoryMax=%[2]s
# for compatibility with older versions of systemd
MemoryLimit=%[2]s

# Always enable task accounting in order to be able to count the processes/
# threads, etc for a slice
TasksAccounting=true
`

	sliceContent := fmt.Sprintf(sliceTempl, grp.Name, memLimit.String())

	exp := []changesObservation{
		{
			snapName: "hello-snap",
			unitType: "service",
			name:     "svc1",
			old:      "",
			new:      svcContent,
		},
		{
			grp:      grp,
			unitType: "slice",
			new:      sliceContent,
			old:      "",
			name:     "foogroup",
		},
	}
	r, observe := expChangeObserver(c, exp)
	defer r()

	err = wrappers.EnsureSnapServices(m, nil, observe, progress.Null)
	c.Assert(err, IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"daemon-reload"},
	})

	c.Assert(svcFile, testutil.FileEquals, svcContent)
}

type changesObservation struct {
	snapName string
	grp      *quota.Group
	unitType string
	name     string
	old      string
	new      string
}

func expChangeObserver(c *C, exp []changesObservation) (restore func(), obs wrappers.ObserveChangeCallback) {
	changesObserved := []changesObservation{}
	f := func(app *snap.AppInfo, grp *quota.Group, unitType, name, old, new string) {
		snapName := ""
		if app != nil {
			snapName = app.Snap.SnapName()
		}
		changesObserved = append(changesObserved, changesObservation{
			snapName: snapName,
			grp:      grp,
			unitType: unitType,
			name:     name,
			old:      old,
			new:      new,
		})
	}

	r := func() {
		// sort the changesObserved by snapName, with all services being
		// observed first, then all groups secondly
		groupObservations := make([]changesObservation, 0, len(changesObserved))
		svcObservations := make([]changesObservation, 0, len(changesObserved))

		for _, chg := range changesObserved {
			if chg.unitType == "slice" {
				groupObservations = append(groupObservations, chg)
			} else {
				svcObservations = append(svcObservations, chg)
			}
		}
		// sort svcObservations, note we do not need to sort groups, since
		// quota groups are iterated over in a stable sorted order via
		// QuotaGroupSet.AllQuotaGroups
		sort.SliceStable(svcObservations, func(i, j int) bool {
			return svcObservations[i].snapName < svcObservations[j].snapName
		})
		finalSortChangesObserved := make([]changesObservation, 0, len(changesObserved))
		finalSortChangesObserved = append(finalSortChangesObserved, svcObservations...)
		finalSortChangesObserved = append(finalSortChangesObserved, groupObservations...)
		c.Assert(finalSortChangesObserved, DeepEquals, exp)
	}

	return r, f
}

func (s *servicesTestSuite) TestEnsureSnapServicesRewritesQuotaSlices(c *C) {
	info := snaptest.MockSnap(c, packageHello, &snap.SideInfo{Revision: snap.R(12)})
	svcFile := filepath.Join(s.tempdir, "/etc/systemd/system/snap.hello-snap.svc1.service")

	memLimit1 := quantity.SizeGiB
	memLimit2 := quantity.SizeGiB * 2

	// write both the unit file and a slice with a different memory limit
	// setting than will be provided below
	sliceTempl := `[Unit]
Description=Slice for snap quota group %s
Before=slices.target
X-Snappy=yes

[Slice]
# Always enable memory accounting otherwise the MemoryMax setting does nothing.
MemoryAccounting=true
MemoryMax=%[2]s
# for compatibility with older versions of systemd
MemoryLimit=%[2]s

# Always enable task accounting in order to be able to count the processes/
# threads, etc for a slice
TasksAccounting=true
`
	sliceFile := filepath.Join(s.tempdir, "/etc/systemd/system/snap.foogroup.slice")

	dir := filepath.Join(dirs.SnapMountDir, "hello-snap", "12.mount")
	svcContent := fmt.Sprintf(`[Unit]
# Auto-generated, DO NOT EDIT
Description=Service for snap application hello-snap.svc1
Requires=%[1]s
Wants=network.target
After=%[1]s network.target snapd.apparmor.service
X-Snappy=yes

[Service]
EnvironmentFile=-/etc/environment
ExecStart=/usr/bin/snap run hello-snap.svc1
SyslogIdentifier=hello-snap.svc1
Restart=on-failure
WorkingDirectory=%[2]s/var/snap/hello-snap/12
ExecStop=/usr/bin/snap run --command=stop hello-snap.svc1
ExecStopPost=/usr/bin/snap run --command=post-stop hello-snap.svc1
TimeoutStopSec=30
Type=forking
Slice=snap.foogroup.slice

[Install]
WantedBy=multi-user.target
`,
		systemd.EscapeUnitNamePath(dir),
		dirs.GlobalRootDir,
	)

	err := os.MkdirAll(filepath.Dir(sliceFile), 0755)
	c.Assert(err, IsNil)

	oldContent := fmt.Sprintf(sliceTempl, "foogroup", memLimit1.String())
	err = ioutil.WriteFile(sliceFile, []byte(oldContent), 0644)
	c.Assert(err, IsNil)

	err = ioutil.WriteFile(svcFile, []byte(svcContent), 0644)
	c.Assert(err, IsNil)

	// use new memory limit
	grp, err := quota.NewGroup("foogroup", memLimit2)
	c.Assert(err, IsNil)

	m := map[*snap.Info]*wrappers.SnapServiceOptions{
		info: {QuotaGroup: grp},
	}

	newContent := fmt.Sprintf(sliceTempl, "foogroup", memLimit2.String())
	exp := []changesObservation{
		{
			grp:      grp,
			unitType: "slice",
			new:      newContent,
			old:      oldContent,
			name:     "foogroup",
		},
	}
	r, observe := expChangeObserver(c, exp)
	defer r()

	err = wrappers.EnsureSnapServices(m, nil, observe, progress.Null)
	c.Assert(err, IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"daemon-reload"},
	})

	c.Assert(svcFile, testutil.FileEquals, svcContent)

	c.Assert(sliceFile, testutil.FileEquals, newContent)

}

func (s *servicesTestSuite) TestEnsureSnapServicesDoesNotRewriteQuotaSlicesOnNoop(c *C) {
	info := snaptest.MockSnap(c, packageHello, &snap.SideInfo{Revision: snap.R(12)})
	svcFile := filepath.Join(s.tempdir, "/etc/systemd/system/snap.hello-snap.svc1.service")

	memLimit := quantity.SizeGiB

	// write both the unit file and a slice before running the ensure
	sliceTempl := `[Unit]
Description=Slice for snap quota group %s
Before=slices.target
X-Snappy=yes

[Slice]
# Always enable memory accounting otherwise the MemoryMax setting does nothing.
MemoryAccounting=true
MemoryMax=%[2]s
# for compatibility with older versions of systemd
MemoryLimit=%[2]s

# Always enable task accounting in order to be able to count the processes/
# threads, etc for a slice
TasksAccounting=true
`
	sliceFile := filepath.Join(s.tempdir, "/etc/systemd/system/snap.foogroup.slice")

	dir := filepath.Join(dirs.SnapMountDir, "hello-snap", "12.mount")
	svcContent := fmt.Sprintf(`[Unit]
# Auto-generated, DO NOT EDIT
Description=Service for snap application hello-snap.svc1
Requires=%[1]s
Wants=network.target
After=%[1]s network.target snapd.apparmor.service
X-Snappy=yes

[Service]
EnvironmentFile=-/etc/environment
ExecStart=/usr/bin/snap run hello-snap.svc1
SyslogIdentifier=hello-snap.svc1
Restart=on-failure
WorkingDirectory=%[2]s/var/snap/hello-snap/12
ExecStop=/usr/bin/snap run --command=stop hello-snap.svc1
ExecStopPost=/usr/bin/snap run --command=post-stop hello-snap.svc1
TimeoutStopSec=30
Type=forking
Slice=snap.foogroup.slice

[Install]
WantedBy=multi-user.target
`,
		systemd.EscapeUnitNamePath(dir),
		dirs.GlobalRootDir,
	)

	err := os.MkdirAll(filepath.Dir(sliceFile), 0755)
	c.Assert(err, IsNil)

	err = ioutil.WriteFile(sliceFile, []byte(fmt.Sprintf(sliceTempl, "foogroup", memLimit.String())), 0644)
	c.Assert(err, IsNil)

	err = ioutil.WriteFile(svcFile, []byte(svcContent), 0644)
	c.Assert(err, IsNil)

	grp, err := quota.NewGroup("foogroup", memLimit)
	c.Assert(err, IsNil)

	m := map[*snap.Info]*wrappers.SnapServiceOptions{
		info: {QuotaGroup: grp},
	}

	observe := func(app *snap.AppInfo, grp *quota.Group, unitType, name, old, new string) {
		c.Error("unexpected call to observe function")
	}

	err = wrappers.EnsureSnapServices(m, nil, observe, progress.Null)
	c.Assert(err, IsNil)
	// no daemon restart since the files didn't change
	c.Check(s.sysdLog, HasLen, 0)

	c.Assert(svcFile, testutil.FileEquals, svcContent)

	c.Assert(sliceFile, testutil.FileEquals, fmt.Sprintf(sliceTempl, "foogroup", memLimit.String()))
}

func (s *servicesTestSuite) TestRemoveQuotaGroup(c *C) {
	// create the group
	grp, err := quota.NewGroup("foogroup", quantity.SizeKiB)
	c.Assert(err, IsNil)

	sliceFile := filepath.Join(s.tempdir, "/etc/systemd/system/snap.foogroup.slice")
	c.Assert(sliceFile, testutil.FileAbsent)

	// removing the group when the slice file doesn't exist is not an error
	err = wrappers.RemoveQuotaGroup(grp, progress.Null)
	c.Assert(err, IsNil)

	c.Assert(s.sysdLog, HasLen, 0)

	c.Assert(sliceFile, testutil.FileAbsent)

	// now write slice file and ensure it is deleted
	sliceTempl := `[Unit]
Description=Slice for snap quota group %s
Before=slices.target
X-Snappy=yes

[Slice]
# Always enable memory accounting otherwise the MemoryMax setting does nothing.
MemoryAccounting=true
MemoryMax=1024
# for compatibility with older versions of systemd
MemoryLimit=1024
`

	err = os.MkdirAll(filepath.Dir(sliceFile), 0755)
	c.Assert(err, IsNil)

	err = ioutil.WriteFile(sliceFile, []byte(fmt.Sprintf(sliceTempl, "foogroup")), 0644)
	c.Assert(err, IsNil)

	// removing it deletes it
	err = wrappers.RemoveQuotaGroup(grp, progress.Null)
	c.Assert(err, IsNil)

	c.Assert(s.sysdLog, DeepEquals, [][]string{
		{"daemon-reload"},
	})

	c.Assert(sliceFile, testutil.FileAbsent)
}

func (s *servicesTestSuite) TestEnsureSnapServicesWithSubGroupQuotaGroupsForSnaps(c *C) {
	info1 := snaptest.MockSnap(c, packageHello, &snap.SideInfo{Revision: snap.R(12)})
	info2 := snaptest.MockSnap(c, `
name: hello-other-snap
version: 1.10
summary: hello
description: Hello...
apps:
 hello:
   command: bin/hello
 world:
   command: bin/world
   completer: world-completer.sh
 svc1:
  command: bin/hello
  stop-command: bin/goodbye
  post-stop-command: bin/missya
  daemon: forking
`, &snap.SideInfo{Revision: snap.R(12)})
	svcFile1 := filepath.Join(s.tempdir, "/etc/systemd/system/snap.hello-snap.svc1.service")
	svcFile2 := filepath.Join(s.tempdir, "/etc/systemd/system/snap.hello-other-snap.svc1.service")

	var err error
	memLimit := quantity.SizeGiB
	// make a root quota group and add the first snap to it
	grp, err := quota.NewGroup("foogroup", memLimit)
	c.Assert(err, IsNil)

	// the second group is a sub-group with the same limit, but is for the
	// second snap
	subgrp, err := grp.NewSubGroup("subgroup", memLimit)
	c.Assert(err, IsNil)

	sliceFile := filepath.Join(s.tempdir, "/etc/systemd/system/snap.foogroup.slice")
	subSliceFile := filepath.Join(s.tempdir, "/etc/systemd/system/snap.foogroup-subgroup.slice")

	m := map[*snap.Info]*wrappers.SnapServiceOptions{
		info1: {QuotaGroup: grp},
		info2: {QuotaGroup: subgrp},
	}

	sliceTempl := `[Unit]
Description=Slice for snap quota group %s
Before=slices.target
X-Snappy=yes

[Slice]
# Always enable memory accounting otherwise the MemoryMax setting does nothing.
MemoryAccounting=true
MemoryMax=%[2]s
# for compatibility with older versions of systemd
MemoryLimit=%[2]s

# Always enable task accounting in order to be able to count the processes/
# threads, etc for a slice
TasksAccounting=true
`

	sliceContent := fmt.Sprintf(sliceTempl, "foogroup", memLimit.String())
	subSliceContent := fmt.Sprintf(sliceTempl, "subgroup", memLimit.String())

	svcTemplate := `[Unit]
# Auto-generated, DO NOT EDIT
Description=Service for snap application %[1]s.svc1
Requires=%[2]s
Wants=network.target
After=%[2]s network.target snapd.apparmor.service
X-Snappy=yes

[Service]
EnvironmentFile=-/etc/environment
ExecStart=/usr/bin/snap run %[1]s.svc1
SyslogIdentifier=%[1]s.svc1
Restart=on-failure
WorkingDirectory=%[3]s/var/snap/%[1]s/12
ExecStop=/usr/bin/snap run --command=stop %[1]s.svc1
ExecStopPost=/usr/bin/snap run --command=post-stop %[1]s.svc1
TimeoutStopSec=30
Type=forking
Slice=%[4]s

[Install]
WantedBy=multi-user.target
`

	dir1 := filepath.Join(dirs.SnapMountDir, "hello-snap", "12.mount")
	dir2 := filepath.Join(dirs.SnapMountDir, "hello-other-snap", "12.mount")

	helloSnapContent := fmt.Sprintf(svcTemplate,
		"hello-snap",
		systemd.EscapeUnitNamePath(dir1),
		dirs.GlobalRootDir,
		"snap.foogroup.slice",
	)

	helloOtherSnapContent := fmt.Sprintf(svcTemplate,
		"hello-other-snap",
		systemd.EscapeUnitNamePath(dir2),
		dirs.GlobalRootDir,
		"snap.foogroup-subgroup.slice",
	)

	exp := []changesObservation{
		{
			snapName: "hello-other-snap",
			unitType: "service",
			name:     "svc1",
			old:      "",
			new:      helloOtherSnapContent,
		},
		{
			snapName: "hello-snap",
			unitType: "service",
			name:     "svc1",
			old:      "",
			new:      helloSnapContent,
		},
		{
			grp:      grp,
			unitType: "slice",
			new:      sliceContent,
			old:      "",
			name:     "foogroup",
		},
		{
			grp:      subgrp,
			unitType: "slice",
			new:      subSliceContent,
			old:      "",
			name:     "subgroup",
		},
	}
	r, observe := expChangeObserver(c, exp)
	defer r()

	err = wrappers.EnsureSnapServices(m, nil, observe, progress.Null)
	c.Assert(err, IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"daemon-reload"},
	})

	c.Assert(svcFile1, testutil.FileEquals, helloSnapContent)

	c.Assert(svcFile2, testutil.FileEquals, helloOtherSnapContent)

	// check that the slice units were also generated

	c.Assert(sliceFile, testutil.FileEquals, sliceContent)
	c.Assert(subSliceFile, testutil.FileEquals, subSliceContent)
}

func (s *servicesTestSuite) TestEnsureSnapServicesWithSubGroupQuotaGroupsGeneratesParentGroups(c *C) {
	info := snaptest.MockSnap(c, packageHello, &snap.SideInfo{Revision: snap.R(12)})
	svcFile1 := filepath.Join(s.tempdir, "/etc/systemd/system/snap.hello-snap.svc1.service")

	var err error
	memLimit := quantity.SizeGiB
	// make a root quota group without any snaps in it
	grp, err := quota.NewGroup("foogroup", memLimit)
	c.Assert(err, IsNil)

	// the second group is a sub-group with the same limit, but it is the one
	// with the snap in it
	subgrp, err := grp.NewSubGroup("subgroup", memLimit)
	c.Assert(err, IsNil)

	sliceFile := filepath.Join(s.tempdir, "/etc/systemd/system/snap.foogroup.slice")
	subSliceFile := filepath.Join(s.tempdir, "/etc/systemd/system/snap.foogroup-subgroup.slice")

	m := map[*snap.Info]*wrappers.SnapServiceOptions{
		info: {QuotaGroup: subgrp},
	}

	err = wrappers.EnsureSnapServices(m, nil, nil, progress.Null)
	c.Assert(err, IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"daemon-reload"},
	})

	svcTemplate := `[Unit]
# Auto-generated, DO NOT EDIT
Description=Service for snap application %[1]s.svc1
Requires=%[2]s
Wants=network.target
After=%[2]s network.target snapd.apparmor.service
X-Snappy=yes

[Service]
EnvironmentFile=-/etc/environment
ExecStart=/usr/bin/snap run %[1]s.svc1
SyslogIdentifier=%[1]s.svc1
Restart=on-failure
WorkingDirectory=%[3]s/var/snap/%[1]s/12
ExecStop=/usr/bin/snap run --command=stop %[1]s.svc1
ExecStopPost=/usr/bin/snap run --command=post-stop %[1]s.svc1
TimeoutStopSec=30
Type=forking
Slice=%[4]s

[Install]
WantedBy=multi-user.target
`

	dir1 := filepath.Join(dirs.SnapMountDir, "hello-snap", "12.mount")

	c.Assert(svcFile1, testutil.FileEquals, fmt.Sprintf(svcTemplate,
		"hello-snap",
		systemd.EscapeUnitNamePath(dir1),
		dirs.GlobalRootDir,
		"snap.foogroup-subgroup.slice",
	))

	// check that both the parent and sub-group slice units were generated
	templ := `[Unit]
Description=Slice for snap quota group %s
Before=slices.target
X-Snappy=yes

[Slice]
# Always enable memory accounting otherwise the MemoryMax setting does nothing.
MemoryAccounting=true
MemoryMax=%[2]s
# for compatibility with older versions of systemd
MemoryLimit=%[2]s

# Always enable task accounting in order to be able to count the processes/
# threads, etc for a slice
TasksAccounting=true
`

	c.Assert(sliceFile, testutil.FileEquals, fmt.Sprintf(templ, "foogroup", memLimit.String()))
	c.Assert(subSliceFile, testutil.FileEquals, fmt.Sprintf(templ, "subgroup", memLimit.String()))
}

func (s *servicesTestSuite) TestEnsureSnapServiceEnsureError(c *C) {
	info := snaptest.MockSnap(c, packageHello, &snap.SideInfo{Revision: snap.R(12)})
	svcFileDir := filepath.Join(s.tempdir, "/etc/systemd/system")

	m := map[*snap.Info]*wrappers.SnapServiceOptions{
		info: nil,
	}

	// make the directory where the service file is written not writable, this
	// will make EnsureFileState return an error
	err := os.MkdirAll(svcFileDir, 0755)
	c.Assert(err, IsNil)

	err = os.Chmod(svcFileDir, 0644)
	c.Assert(err, IsNil)

	err = wrappers.EnsureSnapServices(m, nil, nil, progress.Null)
	c.Assert(err, ErrorMatches, ".* permission denied")
	// we don't issue a daemon-reload since we didn't actually end up making any
	// changes (there was nothing to rollback to)
	c.Check(s.sysdLog, HasLen, 0)

	// we didn't write any files
	c.Assert(filepath.Join(svcFileDir, "snap.hello-snap.svc1.service"), testutil.FileAbsent)
}

func (s *servicesTestSuite) TestEnsureSnapServicesPreseedingHappy(c *C) {
	// map unit -> new
	seen := make(map[string]bool)
	cb := func(app *snap.AppInfo, grp *quota.Group, unitType, name string, old, new string) {
		seen[fmt.Sprintf("%s:%s:%s:%s", app.Snap.InstanceName(), app.Name, unitType, name)] = old == ""
	}

	info := snaptest.MockSnap(c, packageHello, &snap.SideInfo{Revision: snap.R(12)})
	svcFile := filepath.Join(s.tempdir, "/etc/systemd/system/snap.hello-snap.svc1.service")

	m := map[*snap.Info]*wrappers.SnapServiceOptions{
		info: nil,
	}

	// we provide globally applicable Preseeding option via
	// EnsureSnapServiceOptions
	globalOpts := &wrappers.EnsureSnapServicesOptions{
		Preseeding: true,
	}
	err := wrappers.EnsureSnapServices(m, globalOpts, cb, progress.Null)
	c.Assert(err, IsNil)
	// no daemon-reload's since we are preseeding
	c.Check(s.sysdLog, HasLen, 0)
	c.Check(seen, DeepEquals, map[string]bool{
		"hello-snap:svc1:service:svc1": true,
	})

	dir := filepath.Join(dirs.SnapMountDir, "hello-snap", "12.mount")
	c.Assert(svcFile, testutil.FileEquals, fmt.Sprintf(`[Unit]
# Auto-generated, DO NOT EDIT
Description=Service for snap application hello-snap.svc1
Requires=%[1]s
Wants=network.target
After=%[1]s network.target snapd.apparmor.service
X-Snappy=yes

[Service]
EnvironmentFile=-/etc/environment
ExecStart=/usr/bin/snap run hello-snap.svc1
SyslogIdentifier=hello-snap.svc1
Restart=on-failure
WorkingDirectory=%[2]s/var/snap/hello-snap/12
ExecStop=/usr/bin/snap run --command=stop hello-snap.svc1
ExecStopPost=/usr/bin/snap run --command=post-stop hello-snap.svc1
TimeoutStopSec=30
Type=forking

[Install]
WantedBy=multi-user.target
`,
		systemd.EscapeUnitNamePath(dir),
		dirs.GlobalRootDir,
	))
}

func (s *servicesTestSuite) TestEnsureSnapServicesRequireMountedSnapdSnapOptionsHappy(c *C) {
	// map unit -> new
	seen := make(map[string]bool)
	cb := func(app *snap.AppInfo, grp *quota.Group, unitType, name string, old, new string) {
		seen[fmt.Sprintf("%s:%s:%s:%s", app.Snap.InstanceName(), app.Name, unitType, name)] = old == ""
	}

	// use two snaps one with per-snap options and one without to demonstrate
	// that the global options apply to all snaps
	info1 := snaptest.MockSnap(c, packageHello, &snap.SideInfo{Revision: snap.R(12)})
	info2 := snaptest.MockSnap(c, `
name: hello-other-snap
version: 1.10
summary: hello
description: Hello...
apps:
 hello:
   command: bin/hello
 world:
   command: bin/world
   completer: world-completer.sh
 svc1:
  command: bin/hello
  stop-command: bin/goodbye
  post-stop-command: bin/missya
  daemon: forking
`, &snap.SideInfo{Revision: snap.R(12)})
	svcFile1 := filepath.Join(s.tempdir, "/etc/systemd/system/snap.hello-snap.svc1.service")
	svcFile2 := filepath.Join(s.tempdir, "/etc/systemd/system/snap.hello-other-snap.svc1.service")

	// some options per-snap
	m := map[*snap.Info]*wrappers.SnapServiceOptions{
		info1: {VitalityRank: 1},
		info2: nil,
	}

	// and also a global option that should propagate to unit generation too
	globalOpts := &wrappers.EnsureSnapServicesOptions{
		RequireMountedSnapdSnap: true,
	}
	err := wrappers.EnsureSnapServices(m, globalOpts, cb, progress.Null)
	c.Assert(err, IsNil)
	// no daemon-reload's since we are preseeding
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"daemon-reload"},
	})
	c.Check(seen, DeepEquals, map[string]bool{
		"hello-snap:svc1:service:svc1":       true,
		"hello-other-snap:svc1:service:svc1": true,
	})

	template := `[Unit]
# Auto-generated, DO NOT EDIT
Description=Service for snap application %[1]s.svc1
Requires=%[2]s
Wants=network.target
After=%[2]s network.target snapd.apparmor.service
Wants=usr-lib-snapd.mount
After=usr-lib-snapd.mount
X-Snappy=yes

[Service]
EnvironmentFile=-/etc/environment
ExecStart=/usr/bin/snap run %[1]s.svc1
SyslogIdentifier=%[1]s.svc1
Restart=on-failure
WorkingDirectory=%[3]s/var/snap/%[1]s/12
ExecStop=/usr/bin/snap run --command=stop %[1]s.svc1
ExecStopPost=/usr/bin/snap run --command=post-stop %[1]s.svc1
TimeoutStopSec=30
Type=forking
%[4]s
[Install]
WantedBy=multi-user.target
`

	dir1 := filepath.Join(dirs.SnapMountDir, "hello-snap", "12.mount")
	dir2 := filepath.Join(dirs.SnapMountDir, "hello-other-snap", "12.mount")

	c.Assert(svcFile1, testutil.FileEquals, fmt.Sprintf(template,
		"hello-snap",
		systemd.EscapeUnitNamePath(dir1),
		dirs.GlobalRootDir,
		"OOMScoreAdjust=-899\n", // VitalityRank in effect
	))

	c.Assert(svcFile2, testutil.FileEquals, fmt.Sprintf(template,
		"hello-other-snap",
		systemd.EscapeUnitNamePath(dir2),
		dirs.GlobalRootDir,
		"", // no VitalityRank in effect
	))
}

func (s *servicesTestSuite) TestEnsureSnapServicesCallback(c *C) {
	// hava a 2nd new service definition
	info := snaptest.MockSnap(c, packageHello+` svc2:
  command: bin/hello
  stop-command: bin/goodbye
  post-stop-command: bin/missya
  daemon: forking
`, &snap.SideInfo{Revision: snap.R(12)})
	svc1File := filepath.Join(s.tempdir, "/etc/systemd/system/snap.hello-snap.svc1.service")
	svc2File := filepath.Join(s.tempdir, "/etc/systemd/system/snap.hello-snap.svc2.service")

	dir := filepath.Join(dirs.SnapMountDir, "hello-snap", "12.mount")
	template := `[Unit]
# Auto-generated, DO NOT EDIT
Description=Service for snap application hello-snap.%[1]s
Requires=%[2]s
Wants=network.target
After=%[2]s network.target snapd.apparmor.service
X-Snappy=yes

[Service]
EnvironmentFile=-/etc/environment
ExecStart=/usr/bin/snap run hello-snap.%[1]s
SyslogIdentifier=hello-snap.%[1]s
Restart=on-failure
WorkingDirectory=%[3]s/var/snap/hello-snap/12
ExecStop=/usr/bin/snap run --command=stop hello-snap.%[1]s
ExecStopPost=/usr/bin/snap run --command=post-stop hello-snap.%[1]s
TimeoutStopSec=30
Type=forking
%[4]s
[Install]
WantedBy=multi-user.target
`
	svc1Content := fmt.Sprintf(template,
		"svc1",
		systemd.EscapeUnitNamePath(dir),
		dirs.GlobalRootDir,
		"",
	)

	err := os.MkdirAll(filepath.Dir(svc1File), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(svc1File, []byte(svc1Content), 0644)
	c.Assert(err, IsNil)

	// both will be written, one is new
	m := map[*snap.Info]*wrappers.SnapServiceOptions{
		info: {VitalityRank: 1},
	}

	seen := make(map[string][]string)
	cb := func(app *snap.AppInfo, grp *quota.Group, unitType, name string, old, new string) {
		seen[fmt.Sprintf("%s:%s:%s:%s", app.Snap.InstanceName(), app.Name, unitType, name)] = []string{old, new}
	}

	err = wrappers.EnsureSnapServices(m, nil, cb, progress.Null)
	c.Assert(err, IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"daemon-reload"},
	})

	// svc2 was written as expected
	svc2New := fmt.Sprintf(template,
		"svc2",
		systemd.EscapeUnitNamePath(dir),
		dirs.GlobalRootDir,
		"OOMScoreAdjust=-899\n",
	)
	c.Assert(svc2File, testutil.FileEquals, svc2New)

	// and svc1 was changed as well
	svc1New := fmt.Sprintf(template,
		"svc1",
		systemd.EscapeUnitNamePath(dir),
		dirs.GlobalRootDir,
		"OOMScoreAdjust=-899\n",
	)
	c.Assert(svc1File, testutil.FileEquals, svc1New)

	c.Check(seen, DeepEquals, map[string][]string{
		"hello-snap:svc1:service:svc1": {svc1Content, svc1New},
		"hello-snap:svc2:service:svc2": {"", svc2New},
	})
}

func (s *servicesTestSuite) TestEnsureSnapServicesAddsNewSvc(c *C) {
	// map unit -> new
	seen := make(map[string]bool)
	cb := func(app *snap.AppInfo, grp *quota.Group, unitType, name string, old, new string) {
		seen[fmt.Sprintf("%s:%s:%s:%s", app.Snap.InstanceName(), app.Name, unitType, name)] = old == ""
	}

	// test that with an existing service unit definition, it is not changed
	// but we do add the new one
	info := snaptest.MockSnap(c, packageHello+` svc2:
  command: bin/hello
  stop-command: bin/goodbye
  post-stop-command: bin/missya
  daemon: forking
`, &snap.SideInfo{Revision: snap.R(12)})
	svc1File := filepath.Join(s.tempdir, "/etc/systemd/system/snap.hello-snap.svc1.service")
	svc2File := filepath.Join(s.tempdir, "/etc/systemd/system/snap.hello-snap.svc2.service")

	dir := filepath.Join(dirs.SnapMountDir, "hello-snap", "12.mount")
	template := `[Unit]
# Auto-generated, DO NOT EDIT
Description=Service for snap application hello-snap.%[1]s
Requires=%[2]s
Wants=network.target
After=%[2]s network.target snapd.apparmor.service
X-Snappy=yes

[Service]
EnvironmentFile=-/etc/environment
ExecStart=/usr/bin/snap run hello-snap.%[1]s
SyslogIdentifier=hello-snap.%[1]s
Restart=on-failure
WorkingDirectory=%[3]s/var/snap/hello-snap/12
ExecStop=/usr/bin/snap run --command=stop hello-snap.%[1]s
ExecStopPost=/usr/bin/snap run --command=post-stop hello-snap.%[1]s
TimeoutStopSec=30
Type=forking
%[4]s
[Install]
WantedBy=multi-user.target
`
	svc1Content := fmt.Sprintf(template,
		"svc1",
		systemd.EscapeUnitNamePath(dir),
		dirs.GlobalRootDir,
		"",
	)

	err := os.MkdirAll(filepath.Dir(svc1File), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(svc1File, []byte(svc1Content), 0644)
	c.Assert(err, IsNil)

	m := map[*snap.Info]*wrappers.SnapServiceOptions{
		info: nil,
	}

	err = wrappers.EnsureSnapServices(m, nil, cb, progress.Null)
	c.Assert(err, IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"daemon-reload"},
	})
	// we only added svc2
	c.Check(seen, DeepEquals, map[string]bool{
		"hello-snap:svc2:service:svc2": true,
	})

	// svc2 was written as expected
	c.Assert(svc2File, testutil.FileEquals, fmt.Sprintf(template,
		"svc2",
		systemd.EscapeUnitNamePath(dir),
		dirs.GlobalRootDir,
		"",
	))

	// and svc1 didn't change
	c.Assert(svc1File, testutil.FileEquals, fmt.Sprintf(template,
		"svc1",
		systemd.EscapeUnitNamePath(dir),
		dirs.GlobalRootDir,
		"",
	))
}

func (s *servicesTestSuite) TestEnsureSnapServicesNoChangeNoop(c *C) {
	info := snaptest.MockSnap(c, packageHello, &snap.SideInfo{Revision: snap.R(12)})

	// pretend we already have a unit file setup
	svcFile := filepath.Join(s.tempdir, "/etc/systemd/system/snap.hello-snap.svc1.service")

	dir := filepath.Join(dirs.SnapMountDir, "hello-snap", "12.mount")
	template := `[Unit]
# Auto-generated, DO NOT EDIT
Description=Service for snap application hello-snap.svc1
Requires=%[1]s
Wants=network.target
After=%[1]s network.target snapd.apparmor.service
X-Snappy=yes

[Service]
EnvironmentFile=-/etc/environment
ExecStart=/usr/bin/snap run hello-snap.svc1
SyslogIdentifier=hello-snap.svc1
Restart=on-failure
WorkingDirectory=%[2]s/var/snap/hello-snap/12
ExecStop=/usr/bin/snap run --command=stop hello-snap.svc1
ExecStopPost=/usr/bin/snap run --command=post-stop hello-snap.svc1
TimeoutStopSec=30
Type=forking
%s
[Install]
WantedBy=multi-user.target
`
	origContent := fmt.Sprintf(template,
		systemd.EscapeUnitNamePath(dir),
		dirs.GlobalRootDir,
		"",
	)

	err := os.MkdirAll(filepath.Dir(svcFile), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(svcFile, []byte(origContent), 0644)
	c.Assert(err, IsNil)

	// now ensuring with no options will not modify anything or trigger a
	// daemon-reload
	m := map[*snap.Info]*wrappers.SnapServiceOptions{
		info: nil,
	}

	cbCalled := 0
	cb := func(app *snap.AppInfo, grp *quota.Group, unitType, name string, old, new string) {
		cbCalled++
	}

	err = wrappers.EnsureSnapServices(m, nil, cb, progress.Null)
	c.Assert(err, IsNil)
	c.Check(s.sysdLog, HasLen, 0)

	// the file is not changed
	c.Assert(svcFile, testutil.FileEquals, origContent)

	// callback is not called if no change
	c.Check(cbCalled, Equals, 0)
}

func (s *servicesTestSuite) TestEnsureSnapServicesChanges(c *C) {
	// map unit -> new
	seen := make(map[string]bool)
	cb := func(app *snap.AppInfo, grp *quota.Group, unitType, name string, old, new string) {
		seen[fmt.Sprintf("%s:%s:%s:%s", app.Snap.InstanceName(), app.Name, unitType, name)] = old == ""
	}

	info := snaptest.MockSnap(c, packageHello, &snap.SideInfo{Revision: snap.R(12)})

	// pretend we already have a unit file with no VitalityRank options set
	svcFile := filepath.Join(s.tempdir, "/etc/systemd/system/snap.hello-snap.svc1.service")

	dir := filepath.Join(dirs.SnapMountDir, "hello-snap", "12.mount")
	template := `[Unit]
# Auto-generated, DO NOT EDIT
Description=Service for snap application hello-snap.svc1
Requires=%[1]s
Wants=network.target
After=%[1]s network.target snapd.apparmor.service
X-Snappy=yes

[Service]
EnvironmentFile=-/etc/environment
ExecStart=/usr/bin/snap run hello-snap.svc1
SyslogIdentifier=hello-snap.svc1
Restart=on-failure
WorkingDirectory=%[2]s/var/snap/hello-snap/12
ExecStop=/usr/bin/snap run --command=stop hello-snap.svc1
ExecStopPost=/usr/bin/snap run --command=post-stop hello-snap.svc1
TimeoutStopSec=30
Type=forking
%s
[Install]
WantedBy=multi-user.target
`
	origContent := fmt.Sprintf(template,
		systemd.EscapeUnitNamePath(dir),
		dirs.GlobalRootDir,
		"",
	)

	err := os.MkdirAll(filepath.Dir(svcFile), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(svcFile, []byte(origContent), 0644)
	c.Assert(err, IsNil)

	// now ensuring with the VitalityRank set will modify the file
	m := map[*snap.Info]*wrappers.SnapServiceOptions{
		info: {VitalityRank: 1},
	}

	err = wrappers.EnsureSnapServices(m, nil, cb, progress.Null)
	c.Assert(err, IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"daemon-reload"},
	})

	// only modified
	c.Check(seen, DeepEquals, map[string]bool{
		"hello-snap:svc1:service:svc1": false,
	})

	// now the file has been modified to have OOMScoreAdjust set for it
	c.Assert(svcFile, testutil.FileEquals, fmt.Sprintf(template,
		systemd.EscapeUnitNamePath(dir),
		dirs.GlobalRootDir,
		"OOMScoreAdjust=-899\n",
	))
}

func (s *servicesTestSuite) TestEnsureSnapServicesRollsback(c *C) {
	info := snaptest.MockSnap(c, packageHello, &snap.SideInfo{Revision: snap.R(12)})

	svcFile := filepath.Join(s.tempdir, "/etc/systemd/system/snap.hello-snap.svc1.service")

	// pretend we already have a unit file with no VitalityRank options set
	dir := filepath.Join(dirs.SnapMountDir, "hello-snap", "12.mount")
	template := `[Unit]
# Auto-generated, DO NOT EDIT
Description=Service for snap application hello-snap.svc1
Requires=%[1]s
Wants=network.target
After=%[1]s network.target snapd.apparmor.service
X-Snappy=yes

[Service]
EnvironmentFile=-/etc/environment
ExecStart=/usr/bin/snap run hello-snap.svc1
SyslogIdentifier=hello-snap.svc1
Restart=on-failure
WorkingDirectory=%[2]s/var/snap/hello-snap/12
ExecStop=/usr/bin/snap run --command=stop hello-snap.svc1
ExecStopPost=/usr/bin/snap run --command=post-stop hello-snap.svc1
TimeoutStopSec=30
Type=forking
%s
[Install]
WantedBy=multi-user.target
`
	origContent := fmt.Sprintf(template,
		systemd.EscapeUnitNamePath(dir),
		dirs.GlobalRootDir,
		"",
	)

	err := os.MkdirAll(filepath.Dir(svcFile), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(svcFile, []byte(origContent), 0644)
	c.Assert(err, IsNil)

	// make systemctl fail the first time when we try to do a daemon-reload,
	// then the next time don't return an error
	systemctlCalls := 0
	r := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		systemctlCalls++
		switch systemctlCalls {
		case 1:
			// check that the file has been modified to have OOMScoreAdjust set
			// for it
			c.Assert(svcFile, testutil.FileEquals, fmt.Sprintf(template,
				systemd.EscapeUnitNamePath(dir),
				dirs.GlobalRootDir,
				"OOMScoreAdjust=-899\n",
			))

			// now return an error to trigger a rollback
			return nil, fmt.Errorf("oops")
		case 2:
			// check that the rollback happened to restore the original content
			c.Assert(svcFile, testutil.FileEquals, origContent)

			return nil, nil
		default:
			c.Errorf("unexpected call (number %d) to systemctl: %+v", systemctlCalls, cmd)
			return nil, fmt.Errorf("broken test")
		}
	})
	defer r()

	// now ensuring with the VitalityRank set will modify the file
	m := map[*snap.Info]*wrappers.SnapServiceOptions{
		info: {VitalityRank: 1},
	}

	err = wrappers.EnsureSnapServices(m, nil, nil, progress.Null)
	c.Assert(err, ErrorMatches, "oops")
	c.Assert(systemctlCalls, Equals, 2)

	// double-check that after the function is done, the file is back to what we
	// had before (this check duplicates the one in MockSystemctl but doesn't
	// hurt anything to do again)
	c.Assert(svcFile, testutil.FileEquals, origContent)
}

func (s *servicesTestSuite) TestEnsureSnapServicesRemovesNewAddOnRollback(c *C) {
	info := snaptest.MockSnap(c, packageHello, &snap.SideInfo{Revision: snap.R(12)})

	svcFile := filepath.Join(s.tempdir, "/etc/systemd/system/snap.hello-snap.svc1.service")

	// pretend we already have a unit file with no VitalityRank options set
	dir := filepath.Join(dirs.SnapMountDir, "hello-snap", "12.mount")
	template := `[Unit]
# Auto-generated, DO NOT EDIT
Description=Service for snap application hello-snap.svc1
Requires=%[1]s
Wants=network.target
After=%[1]s network.target snapd.apparmor.service
X-Snappy=yes

[Service]
EnvironmentFile=-/etc/environment
ExecStart=/usr/bin/snap run hello-snap.svc1
SyslogIdentifier=hello-snap.svc1
Restart=on-failure
WorkingDirectory=%[2]s/var/snap/hello-snap/12
ExecStop=/usr/bin/snap run --command=stop hello-snap.svc1
ExecStopPost=/usr/bin/snap run --command=post-stop hello-snap.svc1
TimeoutStopSec=30
Type=forking
%s
[Install]
WantedBy=multi-user.target
`
	// make systemctl fail the first time when we try to do a daemon-reload,
	// then the next time don't return an error
	systemctlCalls := 0
	r := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		systemctlCalls++
		switch systemctlCalls {
		case 1:
			// double check that we wrote the new file here before calling
			// daemon reload
			c.Assert(svcFile, testutil.FileEquals, fmt.Sprintf(template,
				systemd.EscapeUnitNamePath(dir),
				dirs.GlobalRootDir,
				"",
			))

			// now return an error to trigger a rollback
			return nil, fmt.Errorf("oops")
		case 2:
			// after the rollback, check that the new file was deleted
			c.Assert(svcFile, testutil.FileAbsent)

			return nil, nil
		default:
			c.Errorf("unexpected call (number %d) to systemctl: %+v", systemctlCalls, cmd)
			return nil, fmt.Errorf("broken test")
		}
	})
	defer r()

	m := map[*snap.Info]*wrappers.SnapServiceOptions{
		info: nil,
	}

	err := wrappers.EnsureSnapServices(m, nil, nil, progress.Null)
	c.Assert(err, ErrorMatches, "oops")
	c.Assert(systemctlCalls, Equals, 2)

	// double-check that after the function is done, the file is gone again
	c.Assert(svcFile, testutil.FileAbsent)
}

func (s *servicesTestSuite) TestEnsureSnapServicesOnlyRemovesNewAddOnRollback(c *C) {
	info := snaptest.MockSnap(c, packageHello+` svc2:
  command: bin/hello
  stop-command: bin/goodbye
  post-stop-command: bin/missya
  daemon: forking
`, &snap.SideInfo{Revision: snap.R(12)})

	// we won't delete existing files, but we will delete new files, so mock an
	// existing file to check that it doesn't get deleted
	svc1File := filepath.Join(s.tempdir, "/etc/systemd/system/snap.hello-snap.svc1.service")
	svc2File := filepath.Join(s.tempdir, "/etc/systemd/system/snap.hello-snap.svc2.service")

	// pretend we already have a unit file with no VitalityRank options set
	dir := filepath.Join(dirs.SnapMountDir, "hello-snap", "12.mount")
	template := `[Unit]
# Auto-generated, DO NOT EDIT
Description=Service for snap application hello-snap.%[1]s
Requires=%[2]s
Wants=network.target
After=%[2]s network.target snapd.apparmor.service
X-Snappy=yes

[Service]
EnvironmentFile=-/etc/environment
ExecStart=/usr/bin/snap run hello-snap.%[1]s
SyslogIdentifier=hello-snap.%[1]s
Restart=on-failure
WorkingDirectory=%[3]s/var/snap/hello-snap/12
ExecStop=/usr/bin/snap run --command=stop hello-snap.%[1]s
ExecStopPost=/usr/bin/snap run --command=post-stop hello-snap.%[1]s
TimeoutStopSec=30
Type=forking
%[4]s
[Install]
WantedBy=multi-user.target
`

	svc1Content := fmt.Sprintf(template,
		"svc1",
		systemd.EscapeUnitNamePath(dir),
		dirs.GlobalRootDir,
		"",
	)
	svc2Content := fmt.Sprintf(template,
		"svc2",
		systemd.EscapeUnitNamePath(dir),
		dirs.GlobalRootDir,
		"",
	)

	err := os.MkdirAll(filepath.Dir(svc1File), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(svc1File, []byte(svc1Content), 0644)
	c.Assert(err, IsNil)

	// make systemctl fail the first time when we try to do a daemon-reload,
	// then the next time don't return an error
	systemctlCalls := 0
	r := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		systemctlCalls++
		switch systemctlCalls {
		case 1:
			// double check that we wrote the new file here before calling
			// daemon reload
			c.Assert(svc2File, testutil.FileEquals, svc2Content)

			// and the existing file is still the same
			c.Assert(svc1File, testutil.FileEquals, svc1Content)

			// now return error to trigger a rollback
			return nil, fmt.Errorf("oops")
		case 2:
			// after the rollback, check that the new file was deleted
			c.Assert(svc2File, testutil.FileAbsent)

			return nil, nil
		default:
			c.Errorf("unexpected call (number %d) to systemctl: %+v", systemctlCalls, cmd)
			return nil, fmt.Errorf("broken test")
		}
	})
	defer r()

	m := map[*snap.Info]*wrappers.SnapServiceOptions{
		info: nil,
	}

	err = wrappers.EnsureSnapServices(m, nil, nil, progress.Null)
	c.Assert(err, ErrorMatches, "oops")
	c.Assert(systemctlCalls, Equals, 2)

	// double-check that after the function, svc2 (the new one) is missing, but
	// svc1 is still the same
	c.Assert(svc2File, testutil.FileAbsent)
	c.Assert(svc1File, testutil.FileEquals, svc1Content)
}

func (s *servicesTestSuite) TestEnsureSnapServicesSubunits(c *C) {
	// map unit -> new
	seen := make(map[string]bool)
	cb := func(app *snap.AppInfo, grp *quota.Group, unitType, name string, old, new string) {
		seen[fmt.Sprintf("%s:%s:%s:%s", app.Snap.InstanceName(), app.Name, unitType, name)] = old == ""
	}

	info := snaptest.MockSnap(c, packageHello+`
  timer: 10:00-12:00
`, &snap.SideInfo{Revision: snap.R(11)})

	m := map[*snap.Info]*wrappers.SnapServiceOptions{
		info: nil,
	}
	err := wrappers.EnsureSnapServices(m, nil, cb, progress.Null)
	c.Assert(err, IsNil)

	c.Check(seen, DeepEquals, map[string]bool{
		"hello-snap:svc1:service:svc1": true,
		"hello-snap:svc1:timer:":       true,
	})
	// reset
	seen = make(map[string]bool)

	// change vitality, timer, add socket
	info = snaptest.MockSnap(c, packageHello+`
  plugs: [network-bind]
  timer: 10:00-12:00,20:00-22:00
  sockets:
    sock1:
      listen-stream: $SNAP_DATA/sock1.socket
`, &snap.SideInfo{Revision: snap.R(12)})

	m = map[*snap.Info]*wrappers.SnapServiceOptions{
		info: {VitalityRank: 1},
	}
	err = wrappers.EnsureSnapServices(m, nil, cb, progress.Null)
	c.Assert(err, IsNil)

	c.Check(seen, DeepEquals, map[string]bool{
		"hello-snap:svc1:service:svc1": false,
		"hello-snap:svc1:timer:":       false,
		"hello-snap:svc1:socket:sock1": true,
	})
}

func (s *servicesTestSuite) TestAddSnapServicesWithInterfaceSnippets(c *C) {
	tt := []struct {
		comment     string
		plugSnippet string
	}{
		// just single bare interfaces with no attributes
		{
			"docker-support",
			`
  plugs:
   - docker-support`,
		},
		{
			"k8s-support",
			`
  plugs:
   - kubernetes-support`,
		},
		{
			"lxd-support",
			`
  plugs:
   - lxd-support
`,
		},
		{
			"greengrass-support",
			`
  plugs:
   - greengrass-support
`,
		},

		// multiple interfaces that require Delegate=true, but only one is
		// generated

		{
			"multiple interfaces that require Delegate=true",
			`
  plugs:
   - docker-support
   - kubernetes-support`,
		},

		// interfaces with flavor attributes

		{
			"k8s-support with kubelet",
			`
  plugs:
   - kubelet
plugs:
 kubelet:
  interface: kubernetes-support
  flavor: kubelet
`,
		},
		{
			"k8s-support with kubeproxy",
			`
  plugs:
   - kubeproxy
plugs:
 kubeproxy:
  interface: kubernetes-support
  flavor: kubeproxy
`,
		},
		{
			"greengrass-support with legacy-container flavor",
			`
  plugs:
   - greengrass
plugs:
 greengrass:
  interface: greengrass-support
  flavor: legacy-container
`,
		},
	}

	for _, t := range tt {
		comment := Commentf(t.comment)
		info := snaptest.MockSnap(c, packageHelloNoSrv+`
 svc1:
  daemon: simple
`+t.plugSnippet,
			&snap.SideInfo{Revision: snap.R(12)})
		svcFile := filepath.Join(s.tempdir, "/etc/systemd/system/snap.hello-snap.svc1.service")

		err := wrappers.AddSnapServices(info, nil, progress.Null)
		c.Assert(err, IsNil, comment)
		c.Check(s.sysdLog, DeepEquals, [][]string{
			{"daemon-reload"},
		}, comment)

		dir := filepath.Join(dirs.SnapMountDir, "hello-snap", "12.mount")
		c.Assert(svcFile, testutil.FileEquals, fmt.Sprintf(`[Unit]
# Auto-generated, DO NOT EDIT
Description=Service for snap application hello-snap.svc1
Requires=%[1]s
Wants=network.target
After=%[1]s network.target snapd.apparmor.service
X-Snappy=yes

[Service]
EnvironmentFile=-/etc/environment
ExecStart=/usr/bin/snap run hello-snap.svc1
SyslogIdentifier=hello-snap.svc1
Restart=on-failure
WorkingDirectory=%[2]s/var/snap/hello-snap/12
TimeoutStopSec=30
Type=simple
Delegate=true

[Install]
WantedBy=multi-user.target
`,
			systemd.EscapeUnitNamePath(dir),
			dirs.GlobalRootDir,
		), comment)

		s.sysdLog = nil
		err = wrappers.StopServices(info.Services(), nil, "", progress.Null, s.perfTimings)
		c.Assert(err, IsNil, comment)
		c.Assert(s.sysdLog, HasLen, 2, comment)
		c.Check(s.sysdLog, DeepEquals, [][]string{
			{"stop", filepath.Base(svcFile)},
			{"show", "--property=ActiveState", "snap.hello-snap.svc1.service"},
		}, comment)

		s.sysdLog = nil
		err = wrappers.RemoveSnapServices(info, progress.Null)
		c.Assert(err, IsNil, comment)
		c.Check(osutil.FileExists(svcFile), Equals, false, comment)
		c.Assert(s.sysdLog, HasLen, 2, comment)
		c.Check(s.sysdLog, DeepEquals, [][]string{
			{"disable", filepath.Base(svcFile)},
			{"daemon-reload"},
		}, comment)

		s.sysdLog = nil
	}
}

func (s *servicesTestSuite) TestAddSnapServicesAndRemoveUserDaemons(c *C) {
	info := snaptest.MockSnap(c, packageHelloNoSrv+`
 svc1:
  daemon: simple
  daemon-scope: user
`, &snap.SideInfo{Revision: snap.R(12)})
	svcFile := filepath.Join(s.tempdir, "/etc/systemd/user/snap.hello-snap.svc1.service")

	err := wrappers.AddSnapServices(info, nil, progress.Null)
	c.Assert(err, IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"--user", "daemon-reload"},
	})

	expected := "ExecStart=/usr/bin/snap run hello-snap.svc1"
	c.Check(svcFile, testutil.FileMatches, "(?ms).*^"+regexp.QuoteMeta(expected)) // check.v1 adds ^ and $ around the regexp provided

	s.sysdLog = nil
	err = wrappers.StopServices(info.Services(), nil, "", progress.Null, s.perfTimings)
	c.Assert(err, IsNil)
	c.Assert(s.sysdLog, HasLen, 2)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"--user", "stop", filepath.Base(svcFile)},
		{"--user", "show", "--property=ActiveState", "snap.hello-snap.svc1.service"},
	})

	s.sysdLog = nil
	err = wrappers.RemoveSnapServices(info, progress.Null)
	c.Assert(err, IsNil)
	c.Check(osutil.FileExists(svcFile), Equals, false)
	c.Assert(s.sysdLog, HasLen, 2)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"--user", "--global", "disable", filepath.Base(svcFile)},
		{"--user", "daemon-reload"},
	})
}

var snapdYaml = `name: snapd
version: 1.0
type: snapd
`

func (s *servicesTestSuite) TestRemoveSnapWithSocketsRemovesSocketsService(c *C) {
	info := snaptest.MockSnap(c, packageHelloNoSrv+`
 svc1:
  daemon: simple
  plugs: [network-bind]
  sockets:
    sock1:
      listen-stream: $SNAP_DATA/sock1.socket
      socket-mode: 0666
    sock2:
      listen-stream: $SNAP_COMMON/sock2.socket
`, &snap.SideInfo{Revision: snap.R(12)})

	err := wrappers.AddSnapServices(info, nil, progress.Null)
	c.Assert(err, IsNil)

	err = wrappers.StopServices(info.Services(), nil, "", &progress.Null, s.perfTimings)
	c.Assert(err, IsNil)

	err = wrappers.RemoveSnapServices(info, &progress.Null)
	c.Assert(err, IsNil)

	app := info.Apps["svc1"]
	c.Assert(app.Sockets, HasLen, 2)
	for _, socket := range app.Sockets {
		c.Check(osutil.FileExists(socket.File()), Equals, false)
	}
}

func (s *servicesTestSuite) TestRemoveSnapPackageFallbackToKill(c *C) {
	restore := wrappers.MockKillWait(time.Millisecond)
	defer restore()

	var sysdLog [][]string
	r := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		// filter out the "systemctl show" that
		// StopServices generates
		if cmd[0] != "show" {
			sysdLog = append(sysdLog, cmd)
		}
		return []byte("ActiveState=active\n"), nil
	})
	defer r()

	info := snaptest.MockSnap(c, `name: wat
version: 42
apps:
 wat:
   command: wat
   stop-timeout: 20ms
   daemon: forking
`, &snap.SideInfo{Revision: snap.R(11)})

	err := wrappers.AddSnapServices(info, nil, progress.Null)
	c.Assert(err, IsNil)

	sysdLog = nil

	svcFName := "snap.wat.wat.service"

	err = wrappers.StopServices(info.Services(), nil, "", progress.Null, s.perfTimings)
	c.Assert(err, IsNil)

	c.Check(sysdLog, DeepEquals, [][]string{
		{"stop", svcFName},
		// check kill invocations
		{"kill", svcFName, "-s", "TERM", "--kill-who=all"},
		{"kill", svcFName, "-s", "KILL", "--kill-who=all"},
	})
}

func (s *servicesTestSuite) TestRemoveSnapPackageUserDaemonStopFailure(c *C) {
	var sysdLog [][]string
	r := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		// filter out the "systemctl --user show" that
		// StopServices generates
		if cmd[0] == "--user" && cmd[1] != "show" {
			sysdLog = append(sysdLog, cmd)
		}
		if cmd[0] == "--user" && cmd[1] == "stop" {
			return nil, fmt.Errorf("user unit stop failed")
		}
		return []byte("ActiveState=active\n"), nil
	})
	defer r()

	info := snaptest.MockSnap(c, `name: wat
version: 42
apps:
 wat:
   command: wat
   stop-timeout: 20ms
   daemon: forking
   daemon-scope: user
`, &snap.SideInfo{Revision: snap.R(11)})

	err := wrappers.AddSnapServices(info, nil, progress.Null)
	c.Assert(err, IsNil)

	sysdLog = nil

	svcFName := "snap.wat.wat.service"

	err = wrappers.StopServices(info.Services(), nil, "", progress.Null, s.perfTimings)
	c.Check(err, ErrorMatches, "some user services failed to stop")
	c.Check(sysdLog, DeepEquals, [][]string{
		{"--user", "stop", svcFName},
	})
}

func (s *servicesTestSuite) TestServicesEnableState(c *C) {
	info := snaptest.MockSnap(c, packageHello+`
 svc2:
  command: bin/hello
  daemon: forking
 svc3:
  command: bin/hello
  daemon: simple
  daemon-scope: user
`, &snap.SideInfo{Revision: snap.R(12)})
	svc1File := "snap.hello-snap.svc1.service"
	svc2File := "snap.hello-snap.svc2.service"

	s.systemctlRestorer()
	r := testutil.MockCommand(c, "systemctl", `#!/bin/sh
	if [ "$1" = "--root" ]; then
		# shifting by 2 also drops the temp dir arg to --root
	    shift 2
	fi

	case "$1" in
		is-enabled)
			case "$2" in 
			"snap.hello-snap.svc1.service")
				echo "disabled"
				exit 1
				;;
			"snap.hello-snap.svc2.service")
				echo "enabled"
				exit 0
				;;
			*)
				echo "unexpected is-enabled of service $2"
				exit 2
				;;
			esac
	        ;;
	    *)
	        echo "unexpected op $*"
	        exit 2
	esac

	exit 1
	`)
	defer r.Restore()

	states, err := wrappers.ServicesEnableState(info, progress.Null)
	c.Assert(err, IsNil)

	c.Assert(states, DeepEquals, map[string]bool{
		"svc1": false,
		"svc2": true,
	})

	// the calls could be out of order in the list, since iterating over a map
	// is non-deterministic, so manually check each call
	c.Assert(r.Calls(), HasLen, 2)
	for _, call := range r.Calls() {
		c.Assert(call, HasLen, 3)
		c.Assert(call[:2], DeepEquals, []string{"systemctl", "is-enabled"})
		switch call[2] {
		case svc1File, svc2File:
		default:
			c.Errorf("unknown service for systemctl call: %s", call[2])
		}
	}
}

func (s *servicesTestSuite) TestServicesEnableStateFail(c *C) {
	info := snaptest.MockSnap(c, packageHello, &snap.SideInfo{Revision: snap.R(12)})
	svc1File := "snap.hello-snap.svc1.service"

	s.systemctlRestorer()
	r := testutil.MockCommand(c, "systemctl", `#!/bin/sh
	if [ "$1" = "--root" ]; then
		# shifting by 2 also drops the temp dir arg to --root
	    shift 2
	fi

	case "$1" in
		is-enabled)
			case "$2" in
			"snap.hello-snap.svc1.service")
				echo "whoops"
				exit 1
				;;
			*)
				echo "unexpected is-enabled of service $2"
				exit 2
				;;
			esac
	        ;;
	    *)
	        echo "unexpected op $*"
	        exit 2
	esac

	exit 1
	`)
	defer r.Restore()

	_, err := wrappers.ServicesEnableState(info, progress.Null)
	c.Assert(err, ErrorMatches, ".*is-enabled snap.hello-snap.svc1.service\\] failed with exit status 1: whoops\n.*")

	c.Assert(r.Calls(), DeepEquals, [][]string{
		{"systemctl", "is-enabled", svc1File},
	})
}

func (s *servicesTestSuite) TestAddSnapServicesWithDisabledServices(c *C) {
	info := snaptest.MockSnap(c, packageHello+`
 svc2:
  command: bin/hello
  daemon: forking
`, &snap.SideInfo{Revision: snap.R(12)})

	s.systemctlRestorer()
	r := testutil.MockCommand(c, "systemctl", `#!/bin/sh
	if [ "$1" = "--root" ]; then
	    shift 2
	fi

	case "$1" in
		enable)
			case "$2" in 
				"snap.hello-snap.svc1.service")
					echo "unexpected enable of disabled service $2"
					exit 1
					;;
				"snap.hello-snap.svc2.service")
					exit 0
					;;
				*)
					echo "unexpected enable of service $2"
					exit 1
					;;
			esac
			;;
		start)
			case "$2" in
				"snap.hello-snap.svc2.service")
					exit 0
					;;
			*)
					echo "unexpected start of service $2"
					exit 1
					;;
			esac
			;;
		daemon-reload)
			exit 0
			;;
	    *)
	        echo "unexpected op $*"
	        exit 2
	esac
	exit 2
	`)
	defer r.Restore()

	// svc1 will be disabled
	disabledSvcs := []string{"svc1"}

	err := wrappers.AddSnapServices(info, nil, progress.Null)
	c.Assert(err, IsNil)

	c.Assert(r.Calls(), DeepEquals, [][]string{
		{"systemctl", "daemon-reload"},
	})

	r.ForgetCalls()

	flags := &wrappers.StartServicesFlags{Enable: true}
	err = wrappers.StartServices(info.Services(), disabledSvcs, flags, progress.Null, s.perfTimings)
	c.Assert(err, IsNil)

	// only svc2 should be enabled
	c.Assert(r.Calls(), DeepEquals, [][]string{
		{"systemctl", "enable", "snap.hello-snap.svc2.service"},
		{"systemctl", "start", "snap.hello-snap.svc2.service"},
	})
}

func (s *servicesTestSuite) TestAddSnapServicesWithPreseed(c *C) {
	opts := &wrappers.AddSnapServicesOptions{Preseeding: true}

	info := snaptest.MockSnap(c, packageHello, &snap.SideInfo{Revision: snap.R(12)})

	s.systemctlRestorer()
	r := testutil.MockCommand(c, "systemctl", "exit 1")
	defer r.Restore()

	err := wrappers.AddSnapServices(info, opts, progress.Null)
	c.Assert(err, IsNil)

	// file was created
	svcFiles, _ := filepath.Glob(filepath.Join(dirs.SnapServicesDir, "snap.*.service"))
	c.Check(svcFiles, HasLen, 1)

	// but systemctl was not called
	c.Assert(r.Calls(), HasLen, 0)
}

func (s *servicesTestSuite) TestStopServicesWithSockets(c *C) {
	var sysServices, userServices []string
	r := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		if cmd[0] == "stop" {
			sysServices = append(sysServices, cmd[1])
		} else if cmd[0] == "--user" && cmd[1] == "stop" {
			userServices = append(userServices, cmd[2])
		}
		return []byte("ActiveState=inactive\n"), nil
	})
	defer r()

	info := snaptest.MockSnap(c, packageHelloNoSrv+`
 svc1:
  daemon: simple
  plugs: [network-bind]
  sockets:
    sock1:
      listen-stream: $SNAP_COMMON/sock1.socket
      socket-mode: 0666
    sock2:
      listen-stream: $SNAP_DATA/sock2.socket
 svc2:
  daemon: simple
  daemon-scope: user
  plugs: [network-bind]
  sockets:
    sock1:
      listen-stream: $SNAP_USER_COMMON/sock1.socket
      socket-mode: 0666
    sock2:
      listen-stream: $SNAP_USER_DATA/sock2.socket
`, &snap.SideInfo{Revision: snap.R(12)})

	err := wrappers.AddSnapServices(info, nil, progress.Null)
	c.Assert(err, IsNil)

	sysServices = nil
	userServices = nil

	err = wrappers.StopServices(info.Services(), nil, "", &progress.Null, s.perfTimings)
	c.Assert(err, IsNil)

	sort.Strings(sysServices)
	c.Check(sysServices, DeepEquals, []string{
		"snap.hello-snap.svc1.service", "snap.hello-snap.svc1.sock1.socket", "snap.hello-snap.svc1.sock2.socket"})
	sort.Strings(userServices)
	c.Check(userServices, DeepEquals, []string{
		"snap.hello-snap.svc2.service", "snap.hello-snap.svc2.sock1.socket", "snap.hello-snap.svc2.sock2.socket"})
}

func (s *servicesTestSuite) TestStartServices(c *C) {
	info := snaptest.MockSnap(c, packageHello, &snap.SideInfo{Revision: snap.R(12)})
	svcFile := filepath.Join(s.tempdir, "/etc/systemd/system/snap.hello-snap.svc1.service")

	flags := &wrappers.StartServicesFlags{Enable: true}
	err := wrappers.StartServices(info.Services(), nil, flags, &progress.Null, s.perfTimings)
	c.Assert(err, IsNil)

	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"enable", filepath.Base(svcFile)},
		{"start", filepath.Base(svcFile)},
	})
}

func (s *servicesTestSuite) TestStartServicesUserDaemons(c *C) {
	info := snaptest.MockSnap(c, packageHelloNoSrv+`
 svc1:
  daemon: simple
  daemon-scope: user
`, &snap.SideInfo{Revision: snap.R(12)})
	svcFile := filepath.Join(s.tempdir, "/etc/systemd/user/snap.hello-snap.svc1.service")

	flags := &wrappers.StartServicesFlags{Enable: true}
	err := wrappers.StartServices(info.Services(), nil, flags, &progress.Null, s.perfTimings)
	c.Assert(err, IsNil)

	c.Assert(s.sysdLog, DeepEquals, [][]string{
		{"--user", "--global", "enable", filepath.Base(svcFile)},
		{"--user", "start", filepath.Base(svcFile)},
	})
}

func (s *servicesTestSuite) TestStartServicesEnabledConditional(c *C) {
	info := snaptest.MockSnap(c, packageHello, &snap.SideInfo{Revision: snap.R(12)})
	svcFile := filepath.Join(s.tempdir, "/etc/systemd/system/snap.hello-snap.svc1.service")

	flags := &wrappers.StartServicesFlags{}
	c.Check(wrappers.StartServices(info.Services(), nil, flags, progress.Null, s.perfTimings), IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{{"start", filepath.Base(svcFile)}})
}

func (s *servicesTestSuite) TestNoStartDisabledServices(c *C) {
	svc2Name := "snap.hello-snap.svc2.service"

	info := snaptest.MockSnap(c, packageHello+`
 svc2:
  command: bin/hello
  daemon: simple
`, &snap.SideInfo{Revision: snap.R(12)})

	s.systemctlRestorer()
	r := testutil.MockCommand(c, "systemctl", `#!/bin/sh
	if [ "$1" = "--root" ]; then
	    shift 2
	fi

	case "$1" in
		start)
			if [ "$2" = "snap.hello-snap.svc2.service" ]; then
				exit 0
			fi
			echo "unexpected start of service $2"
			exit 1
			;;
		enable)
			if [ "$2" = "snap.hello-snap.svc2.service" ]; then
				exit 0
			fi
			echo "unexpected enable of service $2"
			exit 1
			;;
	    *)
	        echo "unexpected call $*"
	        exit 2
	esac
	`)
	defer r.Restore()

	flags := &wrappers.StartServicesFlags{Enable: true}
	err := wrappers.StartServices(info.Services(), []string{"svc1"}, flags, &progress.Null, s.perfTimings)
	c.Assert(err, IsNil)
	c.Assert(r.Calls(), DeepEquals, [][]string{
		{"systemctl", "enable", svc2Name},
		{"systemctl", "start", svc2Name},
	})
}

func (s *servicesTestSuite) TestAddSnapMultiServicesFailCreateCleanup(c *C) {
	// sanity check: there are no service files
	svcFiles, _ := filepath.Glob(filepath.Join(dirs.SnapServicesDir, "snap.hello-snap.*.service"))
	c.Check(svcFiles, HasLen, 0)

	info := snaptest.MockSnap(c, packageHello+`
 svc2:
  daemon: potato
`, &snap.SideInfo{Revision: snap.R(12)})

	err := wrappers.AddSnapServices(info, nil, progress.Null)
	c.Assert(err, ErrorMatches, ".*potato.*")

	// the services are cleaned up
	svcFiles, _ = filepath.Glob(filepath.Join(dirs.SnapServicesDir, "snap.hello-snap.*.service"))
	c.Check(svcFiles, HasLen, 0)

	// *either* the first service failed validation, and nothing
	// was done, *or* the second one failed, and the first one was
	// enabled before the second failed, and disabled after.
	if len(s.sysdLog) > 0 {
		// the second service failed validation
		c.Check(s.sysdLog, DeepEquals, [][]string{
			{"daemon-reload"},
		})
	}
}

func (s *servicesTestSuite) TestMultiServicesFailEnableCleanup(c *C) {
	var sysdLog [][]string
	svc1Name := "snap.hello-snap.svc1.service"
	svc2Name := "snap.hello-snap.svc2.service"
	numEnables := 0

	// sanity check: there are no service files
	svcFiles, _ := filepath.Glob(filepath.Join(dirs.SnapServicesDir, "snap.hello-snap.*.service"))
	c.Check(svcFiles, HasLen, 0)

	r := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		sysdLog = append(sysdLog, cmd)
		sdcmd := cmd[0]
		if sdcmd == "show" {
			return []byte("ActiveState=inactive"), nil
		}
		if len(cmd) >= 2 {
			sdcmd = cmd[len(cmd)-2]
		}
		switch sdcmd {
		case "enable":
			numEnables++
			switch numEnables {
			case 1:
				if cmd[len(cmd)-1] == svc2Name {
					// the services are being iterated in the "wrong" order
					svc1Name, svc2Name = svc2Name, svc1Name
				}
				return nil, nil
			case 2:
				return nil, fmt.Errorf("failed")
			default:
				panic("expected no more than 2 enables")
			}
		case "disable", "daemon-reload", "stop":
			return nil, nil
		default:
			panic("unexpected systemctl command " + sdcmd)
		}
	})
	defer r()

	info := snaptest.MockSnap(c, packageHello+`
 svc2:
  command: bin/hello
  daemon: simple
`, &snap.SideInfo{Revision: snap.R(12)})

	err := wrappers.AddSnapServices(info, nil, progress.Null)
	c.Assert(err, IsNil)

	svcFiles, _ = filepath.Glob(filepath.Join(dirs.SnapServicesDir, "snap.hello-snap.*.service"))
	c.Check(svcFiles, HasLen, 2)

	flags := &wrappers.StartServicesFlags{Enable: true}
	err = wrappers.StartServices(info.Services(), nil, flags, progress.Null, s.perfTimings)
	c.Assert(err, ErrorMatches, "failed")

	c.Check(sysdLog, DeepEquals, [][]string{
		{"daemon-reload"}, // from AddSnapServices
		{"enable", svc1Name},
		{"enable", svc2Name}, // this one fails
		{"disable", svc1Name},
	})
}

func (s *servicesTestSuite) TestAddSnapMultiServicesStartFailOnSystemdReloadCleanup(c *C) {
	// this test might be overdoing it (it's mostly covering the same ground as the previous one), but ... :-)
	var sysdLog [][]string

	// sanity check: there are no service files
	svcFiles, _ := filepath.Glob(filepath.Join(dirs.SnapServicesDir, "snap.hello-snap.*.service"))
	c.Check(svcFiles, HasLen, 0)

	r := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		sysdLog = append(sysdLog, cmd)
		if cmd[0] == "daemon-reload" {
			return nil, fmt.Errorf("failed")
		}
		c.Fatalf("unexpected systemctl call")
		return nil, nil

	})
	defer r()

	info := snaptest.MockSnap(c, packageHello+`
 svc2:
  command: bin/hello
  daemon: simple
`, &snap.SideInfo{Revision: snap.R(12)})

	err := wrappers.AddSnapServices(info, nil, progress.Null)
	c.Assert(err, ErrorMatches, "failed")

	// the services are cleaned up
	svcFiles, _ = filepath.Glob(filepath.Join(dirs.SnapServicesDir, "snap.hello-snap.*.service"))
	c.Check(svcFiles, HasLen, 0)
	c.Check(sysdLog, DeepEquals, [][]string{
		{"daemon-reload"}, // this one fails
		{"daemon-reload"}, // reload as part of cleanup after removal
	})
}

func (s *servicesTestSuite) TestAddSnapMultiUserServicesFailEnableCleanup(c *C) {
	var sysdLog [][]string

	// sanity check: there are no service files
	svcFiles, _ := filepath.Glob(filepath.Join(dirs.SnapUserServicesDir, "snap.hello-snap.*.service"))
	c.Check(svcFiles, HasLen, 0)

	r := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		sysdLog = append(sysdLog, cmd)
		if len(cmd) >= 1 && cmd[0] == "--user" {
			cmd = cmd[1:]
		}
		if len(cmd) >= 1 && cmd[0] == "--global" {
			cmd = cmd[1:]
		}
		sdcmd := cmd[0]
		if len(cmd) >= 2 {
			sdcmd = cmd[len(cmd)-2]
		}
		switch sdcmd {
		case "daemon-reload":
			return nil, fmt.Errorf("failed")
		default:
			panic("unexpected systemctl command " + sdcmd)
		}
	})
	defer r()

	info := snaptest.MockSnap(c, packageHelloNoSrv+`
 svc1:
  command: bin/hello
  daemon: simple
  daemon-scope: user
 svc2:
  command: bin/hello
  daemon: simple
  daemon-scope: user
`, &snap.SideInfo{Revision: snap.R(12)})

	err := wrappers.AddSnapServices(info, nil, progress.Null)
	c.Assert(err, ErrorMatches, "cannot reload daemon: failed")

	// the services are cleaned up
	svcFiles, _ = filepath.Glob(filepath.Join(dirs.SnapUserServicesDir, "snap.hello-snap.*.service"))
	c.Check(svcFiles, HasLen, 0)
	c.Check(sysdLog, DeepEquals, [][]string{
		{"--user", "daemon-reload"},
		{"--user", "daemon-reload"},
	})
}

func (s *servicesTestSuite) TestAddSnapMultiUserServicesStartFailOnSystemdReloadCleanup(c *C) {
	// this test might be overdoing it (it's mostly covering the same ground as the previous one), but ... :-)
	var sysdLog [][]string
	svc1Name := "snap.hello-snap.svc1.service"
	svc2Name := "snap.hello-snap.svc2.service"

	// sanity check: there are no service files
	svcFiles, _ := filepath.Glob(filepath.Join(dirs.SnapUserServicesDir, "snap.hello-snap.*.service"))
	c.Check(svcFiles, HasLen, 0)

	first := true
	r := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		sysdLog = append(sysdLog, cmd)
		if len(cmd) < 3 {
			return nil, fmt.Errorf("failed")
		}
		if first {
			first = false
			if cmd[len(cmd)-1] == svc2Name {
				// the services are being iterated in the "wrong" order
				svc1Name, svc2Name = svc2Name, svc1Name
			}
		}
		return nil, nil

	})
	defer r()

	info := snaptest.MockSnap(c, packageHelloNoSrv+`
 svc1:
  command: bin/hello
  daemon: simple
  daemon-scope: user
 svc2:
  command: bin/hello
  daemon: simple
  daemon-scope: user
`, &snap.SideInfo{Revision: snap.R(12)})

	err := wrappers.AddSnapServices(info, nil, progress.Null)
	c.Assert(err, ErrorMatches, "cannot reload daemon: failed")

	// the services are cleaned up
	svcFiles, _ = filepath.Glob(filepath.Join(dirs.SnapUserServicesDir, "snap.hello-snap.*.service"))
	c.Check(svcFiles, HasLen, 0)
	c.Check(sysdLog, DeepEquals, [][]string{
		{"--user", "daemon-reload"}, // this one fails
		{"--user", "daemon-reload"}, // so does this one :-)
	})
}

func (s *servicesTestSuite) TestAddSnapSocketFiles(c *C) {
	info := snaptest.MockSnap(c, packageHelloNoSrv+`
 svc1:
  daemon: simple
  plugs: [network-bind]
  sockets:
    sock1:
      listen-stream: $SNAP_COMMON/sock1.socket
      socket-mode: 0666
    sock2:
      listen-stream: $SNAP_DATA/sock2.socket
    sock3:
      listen-stream: $XDG_RUNTIME_DIR/sock3.socket

`, &snap.SideInfo{Revision: snap.R(12)})

	sock1File := filepath.Join(s.tempdir, "/etc/systemd/system/snap.hello-snap.svc1.sock1.socket")
	sock2File := filepath.Join(s.tempdir, "/etc/systemd/system/snap.hello-snap.svc1.sock2.socket")
	sock3File := filepath.Join(s.tempdir, "/etc/systemd/system/snap.hello-snap.svc1.sock3.socket")

	err := wrappers.AddSnapServices(info, nil, progress.Null)
	c.Assert(err, IsNil)

	expected := fmt.Sprintf(
		`[Socket]
Service=snap.hello-snap.svc1.service
FileDescriptorName=sock1
ListenStream=%s
SocketMode=0666

`, filepath.Join(s.tempdir, "/var/snap/hello-snap/common/sock1.socket"))
	c.Check(sock1File, testutil.FileContains, expected)

	expected = fmt.Sprintf(
		`[Socket]
Service=snap.hello-snap.svc1.service
FileDescriptorName=sock2
ListenStream=%s

`, filepath.Join(s.tempdir, "/var/snap/hello-snap/12/sock2.socket"))
	c.Check(sock2File, testutil.FileContains, expected)

	expected = fmt.Sprintf(
		`[Socket]
Service=snap.hello-snap.svc1.service
FileDescriptorName=sock3
ListenStream=%s

`, filepath.Join(s.tempdir, "/run/user/0/snap.hello-snap/sock3.socket"))
	c.Check(sock3File, testutil.FileContains, expected)
}

func (s *servicesTestSuite) TestAddSnapUserSocketFiles(c *C) {
	info := snaptest.MockSnap(c, packageHelloNoSrv+`
 svc1:
  daemon: simple
  daemon-scope: user
  plugs: [network-bind]
  sockets:
    sock1:
      listen-stream: $SNAP_USER_COMMON/sock1.socket
      socket-mode: 0666
    sock2:
      listen-stream: $SNAP_USER_DATA/sock2.socket
    sock3:
      listen-stream: $XDG_RUNTIME_DIR/sock3.socket
`, &snap.SideInfo{Revision: snap.R(12)})

	sock1File := filepath.Join(s.tempdir, "/etc/systemd/user/snap.hello-snap.svc1.sock1.socket")
	sock2File := filepath.Join(s.tempdir, "/etc/systemd/user/snap.hello-snap.svc1.sock2.socket")
	sock3File := filepath.Join(s.tempdir, "/etc/systemd/user/snap.hello-snap.svc1.sock3.socket")

	err := wrappers.AddSnapServices(info, nil, progress.Null)
	c.Assert(err, IsNil)

	expected := `[Socket]
Service=snap.hello-snap.svc1.service
FileDescriptorName=sock1
ListenStream=%h/snap/hello-snap/common/sock1.socket
SocketMode=0666

`
	c.Check(sock1File, testutil.FileContains, expected)

	expected = `[Socket]
Service=snap.hello-snap.svc1.service
FileDescriptorName=sock2
ListenStream=%h/snap/hello-snap/12/sock2.socket

`
	c.Check(sock2File, testutil.FileContains, expected)

	expected = `[Socket]
Service=snap.hello-snap.svc1.service
FileDescriptorName=sock3
ListenStream=%t/snap.hello-snap/sock3.socket

`
	c.Check(sock3File, testutil.FileContains, expected)
}

func (s *servicesTestSuite) TestStartSnapMultiServicesFailStartCleanup(c *C) {
	var sysdLog [][]string
	svc1Name := "snap.hello-snap.svc1.service"
	svc2Name := "snap.hello-snap.svc2.service"

	r := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		sysdLog = append(sysdLog, cmd)
		if len(cmd) >= 2 && cmd[0] == "start" {
			name := cmd[len(cmd)-1]
			if name == svc2Name {
				return nil, fmt.Errorf("failed")
			}
		}
		return []byte("ActiveState=inactive\n"), nil
	})
	defer r()

	info := snaptest.MockSnap(c, packageHello+`
 svc2:
  command: bin/hello
  daemon: simple
`, &snap.SideInfo{Revision: snap.R(12)})

	svcs := info.Services()
	c.Assert(svcs, HasLen, 2)
	if svcs[0].Name == "svc2" {
		svcs[0], svcs[1] = svcs[1], svcs[0]
	}

	flags := &wrappers.StartServicesFlags{Enable: true}
	err := wrappers.StartServices(svcs, nil, flags, &progress.Null, s.perfTimings)
	c.Assert(err, ErrorMatches, "failed")
	c.Assert(sysdLog, HasLen, 10, Commentf("len: %v calls: %v", len(sysdLog), sysdLog))
	c.Check(sysdLog, DeepEquals, [][]string{
		{"enable", svc1Name},
		{"enable", svc2Name},
		{"start", svc1Name},
		{"start", svc2Name}, // one of the services fails
		{"stop", svc2Name},
		{"show", "--property=ActiveState", svc2Name},
		{"stop", svc1Name},
		{"show", "--property=ActiveState", svc1Name},
		{"disable", svc1Name},
		{"disable", svc2Name},
	}, Commentf("calls: %v", sysdLog))
}

func (s *servicesTestSuite) TestStartSnapMultiServicesFailStartCleanupWithSockets(c *C) {
	var sysdLog [][]string
	svc1Name := "snap.hello-snap.svc1.service"
	svc2Name := "snap.hello-snap.svc2.service"
	svc2SocketName := "snap.hello-snap.svc2.sock1.socket"
	svc3Name := "snap.hello-snap.svc3.service"
	svc3SocketName := "snap.hello-snap.svc3.sock1.socket"

	r := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		sysdLog = append(sysdLog, cmd)
		c.Logf("call: %v", cmd)
		if len(cmd) >= 2 && cmd[0] == "start" && cmd[1] == svc3SocketName {
			// svc2 socket fails
			return nil, fmt.Errorf("failed")
		}
		return []byte("ActiveState=inactive\n"), nil
	})
	defer r()

	info := snaptest.MockSnap(c, packageHello+`
 svc2:
  command: bin/hello
  daemon: simple
  sockets:
    sock1:
      listen-stream: $SNAP_COMMON/sock1.socket
      socket-mode: 0666
 svc3:
  command: bin/hello
  daemon: simple
  sockets:
    sock1:
      listen-stream: $SNAP_COMMON/sock1.socket
      socket-mode: 0666
`, &snap.SideInfo{Revision: snap.R(12)})

	// ensure desired order
	apps := []*snap.AppInfo{info.Apps["svc1"], info.Apps["svc2"], info.Apps["svc3"]}

	flags := &wrappers.StartServicesFlags{Enable: true}
	err := wrappers.StartServices(apps, nil, flags, &progress.Null, s.perfTimings)
	c.Assert(err, ErrorMatches, "failed")
	c.Logf("sysdlog: %v", sysdLog)
	c.Assert(sysdLog, HasLen, 18, Commentf("len: %v calls: %v", len(sysdLog), sysdLog))
	c.Check(sysdLog, DeepEquals, [][]string{
		{"enable", svc1Name},
		{"enable", svc2SocketName},
		{"start", svc2SocketName},
		{"enable", svc3SocketName},
		{"start", svc3SocketName}, // start failed, what follows is the cleanup
		{"stop", svc3SocketName},
		{"show", "--property=ActiveState", svc3SocketName},
		{"stop", svc3Name},
		{"show", "--property=ActiveState", svc3Name},
		{"disable", svc3SocketName},
		{"stop", svc2SocketName},
		{"show", "--property=ActiveState", svc2SocketName},
		{"stop", svc2Name},
		{"show", "--property=ActiveState", svc2Name},
		{"disable", svc2SocketName},
		{"stop", svc1Name},
		{"show", "--property=ActiveState", svc1Name},
		{"disable", svc1Name},
	}, Commentf("calls: %v", sysdLog))
}

func (s *servicesTestSuite) TestStartSnapMultiUserServicesFailStartCleanup(c *C) {
	var sysdLog [][]string
	svc1Name := "snap.hello-snap.svc1.service"
	svc2Name := "snap.hello-snap.svc2.service"

	r := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		sysdLog = append(sysdLog, cmd)
		if len(cmd) >= 3 && cmd[0] == "--user" && cmd[1] == "start" {
			name := cmd[len(cmd)-1]
			if name == svc2Name {
				return nil, fmt.Errorf("failed")
			}
		}
		return []byte("ActiveState=inactive\n"), nil
	})
	defer r()

	info := snaptest.MockSnap(c, packageHelloNoSrv+`
 svc1:
  command: bin/hello
  daemon: simple
  daemon-scope: user
 svc2:
  command: bin/hello
  daemon: simple
  daemon-scope: user
`, &snap.SideInfo{Revision: snap.R(12)})

	svcs := info.Services()
	c.Assert(svcs, HasLen, 2)
	if svcs[0].Name == "svc2" {
		svcs[0], svcs[1] = svcs[1], svcs[0]
	}
	flags := &wrappers.StartServicesFlags{Enable: true}
	err := wrappers.StartServices(svcs, nil, flags, &progress.Null, s.perfTimings)
	c.Assert(err, ErrorMatches, "some user services failed to start")
	c.Assert(sysdLog, HasLen, 12, Commentf("len: %v calls: %v", len(sysdLog), sysdLog))
	c.Check(sysdLog, DeepEquals, [][]string{
		{"--user", "--global", "enable", svc1Name},
		{"--user", "--global", "enable", svc2Name},
		{"--user", "start", svc1Name},
		{"--user", "start", svc2Name}, // one of the services fails
		// session agent attempts to stop the non-failed services
		{"--user", "stop", svc1Name},
		{"--user", "show", "--property=ActiveState", svc1Name},
		// StartServices ensures everything is stopped
		{"--user", "stop", svc2Name},
		{"--user", "show", "--property=ActiveState", svc2Name},
		{"--user", "stop", svc1Name},
		{"--user", "show", "--property=ActiveState", svc1Name},
		{"--user", "--global", "disable", svc1Name},
		{"--user", "--global", "disable", svc2Name},
	}, Commentf("calls: %v", sysdLog))
}

func (s *servicesTestSuite) TestStartSnapServicesKeepsOrder(c *C) {
	var sysdLog [][]string
	svc1Name := "snap.services-snap.svc1.service"
	svc2Name := "snap.services-snap.svc2.service"
	svc3Name := "snap.services-snap.svc3.service"

	r := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		sysdLog = append(sysdLog, cmd)
		return []byte("ActiveState=inactive\n"), nil
	})
	defer r()

	info := snaptest.MockSnap(c, `name: services-snap
apps:
  svc1:
    daemon: simple
    before: [svc3]
  svc2:
    daemon: simple
    after: [svc1]
  svc3:
    daemon: simple
    before: [svc2]
`, &snap.SideInfo{Revision: snap.R(12)})

	svcs := info.Services()
	c.Assert(svcs, HasLen, 3)

	sorted, err := snap.SortServices(svcs)
	c.Assert(err, IsNil)

	flags := &wrappers.StartServicesFlags{Enable: true}
	err = wrappers.StartServices(sorted, nil, flags, &progress.Null, s.perfTimings)
	c.Assert(err, IsNil)
	c.Assert(sysdLog, HasLen, 6, Commentf("len: %v calls: %v", len(sysdLog), sysdLog))
	c.Check(sysdLog, DeepEquals, [][]string{
		{"enable", svc1Name},
		{"enable", svc3Name},
		{"enable", svc2Name},
		{"start", svc1Name},
		{"start", svc3Name},
		{"start", svc2Name},
	}, Commentf("calls: %v", sysdLog))

	// change the order
	sorted[1], sorted[0] = sorted[0], sorted[1]

	// we should observe the calls done in the same order as services
	err = wrappers.StartServices(sorted, nil, flags, &progress.Null, s.perfTimings)
	c.Assert(err, IsNil)
	c.Assert(sysdLog, HasLen, 12, Commentf("len: %v calls: %v", len(sysdLog), sysdLog))
	c.Check(sysdLog[6:], DeepEquals, [][]string{
		{"enable", svc3Name},
		{"enable", svc1Name},
		{"enable", svc2Name},
		{"start", svc3Name},
		{"start", svc1Name},
		{"start", svc2Name},
	}, Commentf("calls: %v", sysdLog))
}

func (s *servicesTestSuite) TestServiceAfterBefore(c *C) {
	snapYaml := packageHello + `
 svc2:
   daemon: forking
   after: [svc1]
 svc3:
   daemon: forking
   before: [svc4]
   after:  [svc2]
 svc4:
   daemon: forking
   after:
     - svc1
     - svc2
     - svc3
`
	info := snaptest.MockSnap(c, snapYaml, &snap.SideInfo{Revision: snap.R(12)})

	checks := []struct {
		file    string
		kind    string
		matches []string
	}{{
		file:    filepath.Join(s.tempdir, "/etc/systemd/system/snap.hello-snap.svc2.service"),
		kind:    "After",
		matches: []string{info.Apps["svc1"].ServiceName()},
	}, {
		file:    filepath.Join(s.tempdir, "/etc/systemd/system/snap.hello-snap.svc3.service"),
		kind:    "After",
		matches: []string{info.Apps["svc2"].ServiceName()},
	}, {
		file:    filepath.Join(s.tempdir, "/etc/systemd/system/snap.hello-snap.svc3.service"),
		kind:    "Before",
		matches: []string{info.Apps["svc4"].ServiceName()},
	}, {
		file: filepath.Join(s.tempdir, "/etc/systemd/system/snap.hello-snap.svc4.service"),
		kind: "After",
		matches: []string{
			info.Apps["svc1"].ServiceName(),
			info.Apps["svc2"].ServiceName(),
			info.Apps["svc3"].ServiceName(),
		},
	}}

	err := wrappers.AddSnapServices(info, nil, progress.Null)
	c.Assert(err, IsNil)

	for _, check := range checks {
		for _, m := range check.matches {
			c.Check(check.file, testutil.FileMatches,
				// match:
				//   ...
				//   After=other.mount some.target foo.service bar.service
				//   Before=foo.service bar.service
				//   ...
				// but not:
				//   Foo=something After=foo.service Bar=something else
				// or:
				//   After=foo.service
				//   bar.service
				// or:
				//   After=  foo.service    bar.service
				"(?ms).*^(?U)"+check.kind+"=.*\\s?"+regexp.QuoteMeta(m)+"\\s?[^=]*$")
		}
	}
}

func (s *servicesTestSuite) TestServiceWatchdog(c *C) {
	snapYaml := packageHello + `
 svc2:
   daemon: forking
   watchdog-timeout: 12s
 svc3:
   daemon: forking
   watchdog-timeout: 0s
 svc4:
   daemon: forking
`
	info := snaptest.MockSnap(c, snapYaml, &snap.SideInfo{Revision: snap.R(12)})

	err := wrappers.AddSnapServices(info, nil, progress.Null)
	c.Assert(err, IsNil)

	content, err := ioutil.ReadFile(filepath.Join(s.tempdir, "/etc/systemd/system/snap.hello-snap.svc2.service"))
	c.Assert(err, IsNil)
	c.Check(strings.Contains(string(content), "\nWatchdogSec=12\n"), Equals, true)

	noWatchdog := []string{
		filepath.Join(s.tempdir, "/etc/systemd/system/snap.hello-snap.svc3.service"),
		filepath.Join(s.tempdir, "/etc/systemd/system/snap.hello-snap.svc4.service"),
	}
	for _, svcPath := range noWatchdog {
		content, err := ioutil.ReadFile(svcPath)
		c.Assert(err, IsNil)
		c.Check(strings.Contains(string(content), "WatchdogSec="), Equals, false)
	}
}

func (s *servicesTestSuite) TestStopServiceEndure(c *C) {
	const surviveYaml = `name: survive-snap
version: 1.0
apps:
 survivor:
  command: bin/survivor
  refresh-mode: endure
  daemon: simple
`
	info := snaptest.MockSnap(c, surviveYaml, &snap.SideInfo{Revision: snap.R(1)})
	survivorFile := filepath.Join(s.tempdir, "/etc/systemd/system/snap.survive-snap.survivor.service")

	err := wrappers.AddSnapServices(info, nil, progress.Null)
	c.Assert(err, IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"daemon-reload"},
	})
	s.sysdLog = nil

	apps := []*snap.AppInfo{info.Apps["survivor"]}
	flags := &wrappers.StartServicesFlags{Enable: true}
	err = wrappers.StartServices(apps, nil, flags, progress.Null, s.perfTimings)
	c.Assert(err, IsNil)

	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"enable", filepath.Base(survivorFile)},
		{"start", filepath.Base(survivorFile)},
	})

	s.sysdLog = nil
	err = wrappers.StopServices(info.Services(), nil, snap.StopReasonRefresh, progress.Null, s.perfTimings)
	c.Assert(err, IsNil)
	c.Assert(s.sysdLog, HasLen, 0)

	s.sysdLog = nil
	err = wrappers.StopServices(info.Services(), nil, snap.StopReasonRemove, progress.Null, s.perfTimings)
	c.Assert(err, IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"stop", filepath.Base(survivorFile)},
		{"show", "--property=ActiveState", "snap.survive-snap.survivor.service"},
	})

}

func (s *servicesTestSuite) TestStopServiceSigs(c *C) {
	r := wrappers.MockKillWait(1 * time.Millisecond)
	defer r()

	survivorFile := filepath.Join(s.tempdir, "/etc/systemd/system/snap.survive-snap.srv.service")
	for _, t := range []struct {
		mode        string
		expectedSig string
		expectedWho string
	}{
		{mode: "sigterm", expectedSig: "TERM", expectedWho: "main"},
		{mode: "sigterm-all", expectedSig: "TERM", expectedWho: "all"},
		{mode: "sighup", expectedSig: "HUP", expectedWho: "main"},
		{mode: "sighup-all", expectedSig: "HUP", expectedWho: "all"},
		{mode: "sigusr1", expectedSig: "USR1", expectedWho: "main"},
		{mode: "sigusr1-all", expectedSig: "USR1", expectedWho: "all"},
		{mode: "sigusr2", expectedSig: "USR2", expectedWho: "main"},
		{mode: "sigusr2-all", expectedSig: "USR2", expectedWho: "all"},
	} {
		surviveYaml := fmt.Sprintf(`name: survive-snap
version: 1.0
apps:
 srv:
  command: bin/survivor
  stop-mode: %s
  daemon: simple
`, t.mode)
		info := snaptest.MockSnap(c, surviveYaml, &snap.SideInfo{Revision: snap.R(1)})

		s.sysdLog = nil
		err := wrappers.AddSnapServices(info, nil, progress.Null)
		c.Assert(err, IsNil)

		c.Check(s.sysdLog, DeepEquals, [][]string{
			{"daemon-reload"},
		})
		s.sysdLog = nil

		var apps []*snap.AppInfo
		for _, a := range info.Apps {
			apps = append(apps, a)
		}
		flags := &wrappers.StartServicesFlags{Enable: true}
		err = wrappers.StartServices(apps, nil, flags, progress.Null, s.perfTimings)
		c.Assert(err, IsNil)
		c.Check(s.sysdLog, DeepEquals, [][]string{
			{"enable", filepath.Base(survivorFile)},
			{"start", filepath.Base(survivorFile)},
		})

		s.sysdLog = nil
		err = wrappers.StopServices(info.Services(), nil, snap.StopReasonRefresh, progress.Null, s.perfTimings)
		c.Assert(err, IsNil)
		c.Check(s.sysdLog, DeepEquals, [][]string{
			{"stop", filepath.Base(survivorFile)},
			{"show", "--property=ActiveState", "snap.survive-snap.srv.service"},
		}, Commentf("failure in %s", t.mode))

		s.sysdLog = nil
		err = wrappers.StopServices(info.Services(), nil, snap.StopReasonRemove, progress.Null, s.perfTimings)
		c.Assert(err, IsNil)
		switch t.expectedWho {
		case "all":
			c.Check(s.sysdLog, DeepEquals, [][]string{
				{"stop", filepath.Base(survivorFile)},
				{"show", "--property=ActiveState", "snap.survive-snap.srv.service"},
			})
		case "main":
			c.Check(s.sysdLog, DeepEquals, [][]string{
				{"stop", filepath.Base(survivorFile)},
				{"show", "--property=ActiveState", "snap.survive-snap.srv.service"},
				{"kill", filepath.Base(survivorFile), "-s", "TERM", "--kill-who=all"},
				{"kill", filepath.Base(survivorFile), "-s", "KILL", "--kill-who=all"},
			})
		default:
			panic("not reached")
		}
	}

}

func (s *servicesTestSuite) TestStartSnapSocketEnableStart(c *C) {
	svc1Name := "snap.hello-snap.svc1.service"
	// svc2Name := "snap.hello-snap.svc2.service"
	svc2Sock := "snap.hello-snap.svc2.sock.socket"
	svc3Sock := "snap.hello-snap.svc3.sock.socket"

	info := snaptest.MockSnap(c, packageHello+`
 svc2:
  command: bin/hello
  daemon: simple
  sockets:
    sock:
      listen-stream: $SNAP_COMMON/sock1.socket
 svc3:
  command: bin/hello
  daemon: simple
  daemon-scope: user
  sockets:
    sock:
      listen-stream: $SNAP_USER_COMMON/sock1.socket
`, &snap.SideInfo{Revision: snap.R(12)})

	// fix the apps order to make the test stable
	apps := []*snap.AppInfo{info.Apps["svc1"], info.Apps["svc2"], info.Apps["svc3"]}
	flags := &wrappers.StartServicesFlags{Enable: true}
	err := wrappers.StartServices(apps, nil, flags, &progress.Null, s.perfTimings)
	c.Assert(err, IsNil)
	c.Assert(s.sysdLog, HasLen, 6, Commentf("len: %v calls: %v", len(s.sysdLog), s.sysdLog))
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"enable", svc1Name},
		{"enable", svc2Sock},
		{"start", svc2Sock},
		{"--user", "--global", "enable", svc3Sock},
		{"--user", "start", svc3Sock},
		{"start", svc1Name},
	}, Commentf("calls: %v", s.sysdLog))
}

func (s *servicesTestSuite) TestStartSnapTimerEnableStart(c *C) {
	svc1Name := "snap.hello-snap.svc1.service"
	// svc2Name := "snap.hello-snap.svc2.service"
	svc2Timer := "snap.hello-snap.svc2.timer"
	svc3Timer := "snap.hello-snap.svc3.timer"

	info := snaptest.MockSnap(c, packageHello+`
 svc2:
  command: bin/hello
  daemon: simple
  timer: 10:00-12:00
 svc3:
  command: bin/hello
  daemon: simple
  daemon-scope: user
  timer: 10:00-12:00
`, &snap.SideInfo{Revision: snap.R(12)})

	// fix the apps order to make the test stable
	apps := []*snap.AppInfo{info.Apps["svc1"], info.Apps["svc2"], info.Apps["svc3"]}
	flags := &wrappers.StartServicesFlags{Enable: true}
	err := wrappers.StartServices(apps, nil, flags, &progress.Null, s.perfTimings)
	c.Assert(err, IsNil)
	c.Assert(s.sysdLog, HasLen, 6, Commentf("len: %v calls: %v", len(s.sysdLog), s.sysdLog))
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"enable", svc1Name},
		{"enable", svc2Timer},
		{"start", svc2Timer},
		{"--user", "--global", "enable", svc3Timer},
		{"--user", "start", svc3Timer},
		{"start", svc1Name},
	}, Commentf("calls: %v", s.sysdLog))
}

func (s *servicesTestSuite) TestStartSnapTimerCleanup(c *C) {
	var sysdLog [][]string
	svc1Name := "snap.hello-snap.svc1.service"
	svc2Name := "snap.hello-snap.svc2.service"
	svc2Timer := "snap.hello-snap.svc2.timer"

	r := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		sysdLog = append(sysdLog, cmd)
		if len(cmd) >= 2 && cmd[0] == "start" && cmd[1] == svc2Timer {
			return nil, fmt.Errorf("failed")
		}
		return []byte("ActiveState=inactive\n"), nil
	})
	defer r()

	info := snaptest.MockSnap(c, packageHello+`
 svc2:
  command: bin/hello
  daemon: simple
  timer: 10:00-12:00
`, &snap.SideInfo{Revision: snap.R(12)})

	// fix the apps order to make the test stable
	apps := []*snap.AppInfo{info.Apps["svc1"], info.Apps["svc2"]}
	flags := &wrappers.StartServicesFlags{Enable: true}
	err := wrappers.StartServices(apps, nil, flags, &progress.Null, s.perfTimings)
	c.Assert(err, ErrorMatches, "failed")
	c.Assert(sysdLog, HasLen, 11, Commentf("len: %v calls: %v", len(sysdLog), sysdLog))
	c.Check(sysdLog, DeepEquals, [][]string{
		{"enable", svc1Name},
		{"enable", svc2Timer},
		{"start", svc2Timer}, // this call fails
		{"stop", svc2Timer},
		{"show", "--property=ActiveState", svc2Timer},
		{"stop", svc2Name},
		{"show", "--property=ActiveState", svc2Name},
		{"disable", svc2Timer},
		{"stop", svc1Name},
		{"show", "--property=ActiveState", svc1Name},
		{"disable", svc1Name},
	}, Commentf("calls: %v", sysdLog))
}

func (s *servicesTestSuite) TestAddRemoveSnapWithTimersAddsRemovesTimerFiles(c *C) {
	info := snaptest.MockSnap(c, packageHello+`
 svc2:
  command: bin/hello
  daemon: simple
  timer: 10:00-12:00
`, &snap.SideInfo{Revision: snap.R(12)})

	err := wrappers.AddSnapServices(info, nil, progress.Null)
	c.Assert(err, IsNil)

	app := info.Apps["svc2"]
	c.Assert(app.Timer, NotNil)

	c.Check(osutil.FileExists(app.Timer.File()), Equals, true)
	c.Check(osutil.FileExists(app.ServiceFile()), Equals, true)

	err = wrappers.StopServices(info.Services(), nil, "", &progress.Null, s.perfTimings)
	c.Assert(err, IsNil)

	err = wrappers.RemoveSnapServices(info, &progress.Null)
	c.Assert(err, IsNil)

	c.Check(osutil.FileExists(app.Timer.File()), Equals, false)
	c.Check(osutil.FileExists(app.ServiceFile()), Equals, false)
}

func (s *servicesTestSuite) TestFailedAddSnapCleansUp(c *C) {
	info := snaptest.MockSnap(c, packageHello+`
 svc2:
  command: bin/hello
  daemon: simple
  timer: 10:00-12:00
 svc3:
  command: bin/hello
  daemon: simple
  plugs: [network-bind]
  sockets:
    sock1:
      listen-stream: $SNAP_COMMON/sock1.socket
      socket-mode: 0666
`, &snap.SideInfo{Revision: snap.R(12)})

	calls := 0
	r := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		if len(cmd) == 1 && cmd[0] == "daemon-reload" && calls == 0 {
			// only fail the first systemd daemon-reload call, the
			// second one is at the end of cleanup
			calls += 1
			return nil, fmt.Errorf("failed")
		}
		return []byte("ActiveState=inactive\n"), nil
	})
	defer r()

	err := wrappers.AddSnapServices(info, nil, progress.Null)
	c.Assert(err, NotNil)

	c.Logf("services dir: %v", dirs.SnapServicesDir)
	matches, err := filepath.Glob(dirs.SnapServicesDir + "/*")
	c.Assert(err, IsNil)
	c.Assert(matches, HasLen, 0, Commentf("the following autogenerated files were left behind: %v", matches))
}

func (s *servicesTestSuite) TestAddServicesDidReload(c *C) {
	const base = `name: hello-snap
version: 1.10
summary: hello
description: Hello...
apps:
`
	onlyServices := snaptest.MockSnap(c, base+`
 svc1:
  command: bin/hello
  daemon: simple
`, &snap.SideInfo{Revision: snap.R(12)})

	onlySockets := snaptest.MockSnap(c, base+`
 svc1:
  command: bin/hello
  daemon: simple
  plugs: [network-bind]
  sockets:
    sock1:
      listen-stream: $SNAP_COMMON/sock1.socket
      socket-mode: 0666
`, &snap.SideInfo{Revision: snap.R(12)})

	onlyTimers := snaptest.MockSnap(c, base+`
 svc1:
  command: bin/hello
  daemon: oneshot
  timer: 10:00-12:00
`, &snap.SideInfo{Revision: snap.R(12)})

	for i, info := range []*snap.Info{onlyServices, onlySockets, onlyTimers} {
		s.sysdLog = nil
		err := wrappers.AddSnapServices(info, nil, progress.Null)
		c.Assert(err, IsNil)
		reloads := 0
		c.Logf("calls: %v", s.sysdLog)
		for _, call := range s.sysdLog {
			if strutil.ListContains(call, "daemon-reload") {
				reloads += 1
			}
		}
		c.Check(reloads >= 1, Equals, true, Commentf("test-case %v did not reload services as expected", i))
	}
}

func (s *servicesTestSuite) TestSnapServicesActivation(c *C) {
	const snapYaml = `name: hello-snap
version: 1.10
summary: hello
description: Hello...
apps:
 svc1:
  command: bin/hello
  daemon: simple
  plugs: [network-bind]
  sockets:
    sock1:
      listen-stream: $SNAP_COMMON/sock1.socket
      socket-mode: 0666
 svc2:
  command: bin/hello
  daemon: oneshot
  timer: 10:00-12:00
 svc3:
  command: bin/hello
  daemon: simple
`
	svc1Socket := "snap.hello-snap.svc1.sock1.socket"
	svc2Timer := "snap.hello-snap.svc2.timer"
	svc3Name := "snap.hello-snap.svc3.service"

	info := snaptest.MockSnap(c, snapYaml, &snap.SideInfo{Revision: snap.R(12)})

	// fix the apps order to make the test stable
	err := wrappers.AddSnapServices(info, nil, progress.Null)
	c.Assert(err, IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"daemon-reload"},
	})
	s.sysdLog = nil

	apps := []*snap.AppInfo{info.Apps["svc1"], info.Apps["svc2"], info.Apps["svc3"]}
	flags := &wrappers.StartServicesFlags{Enable: true}
	err = wrappers.StartServices(apps, nil, flags, progress.Null, s.perfTimings)
	c.Assert(err, IsNil)

	c.Assert(s.sysdLog, HasLen, 6, Commentf("len: %v calls: %v", len(s.sysdLog), s.sysdLog))
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"enable", svc3Name},
		{"enable", svc1Socket},
		{"start", svc1Socket},
		{"enable", svc2Timer},
		{"start", svc2Timer},
		{"start", svc3Name},
	}, Commentf("calls: %v", s.sysdLog))
}

func (s *servicesTestSuite) TestServiceRestartDelay(c *C) {
	snapYaml := packageHello + `
 svc2:
   daemon: forking
   restart-delay: 12s
 svc3:
   daemon: forking
`
	info := snaptest.MockSnap(c, snapYaml, &snap.SideInfo{Revision: snap.R(12)})

	err := wrappers.AddSnapServices(info, nil, progress.Null)
	c.Assert(err, IsNil)

	content, err := ioutil.ReadFile(filepath.Join(s.tempdir, "/etc/systemd/system/snap.hello-snap.svc2.service"))
	c.Assert(err, IsNil)
	c.Check(strings.Contains(string(content), "\nRestartSec=12\n"), Equals, true)

	content, err = ioutil.ReadFile(filepath.Join(s.tempdir, "/etc/systemd/system/snap.hello-snap.svc3.service"))
	c.Assert(err, IsNil)
	c.Check(strings.Contains(string(content), "RestartSec="), Equals, false)
}

func (s *servicesTestSuite) TestAddRemoveSnapServiceWithSnapd(c *C) {
	info := makeMockSnapdSnap(c)

	err := wrappers.AddSnapServices(info, nil, progress.Null)
	c.Check(err, ErrorMatches, "internal error: adding explicit services for snapd snap is unexpected")

	err = wrappers.RemoveSnapServices(info, progress.Null)
	c.Check(err, ErrorMatches, "internal error: removing explicit services for snapd snap is unexpected")
}

func (s *servicesTestSuite) TestReloadOrRestart(c *C) {
	const surviveYaml = `name: test-snap
version: 1.0
apps:
  foo:
    command: bin/foo
    daemon: simple
`
	info := snaptest.MockSnap(c, surviveYaml, &snap.SideInfo{Revision: snap.R(1)})
	srvFile := "snap.test-snap.foo.service"

	r := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		s.sysdLog = append(s.sysdLog, cmd)
		if out := systemdtest.HandleMockAllUnitsActiveOutput(cmd, nil); out != nil {
			return out, nil
		}
		return []byte("ActiveState=inactive\n"), nil
	})
	defer r()

	err := wrappers.AddSnapServices(info, nil, progress.Null)
	c.Assert(err, IsNil)

	s.sysdLog = nil
	flags := &wrappers.RestartServicesFlags{Reload: true}
	c.Assert(wrappers.RestartServices(info.Services(), nil, flags, progress.Null, s.perfTimings), IsNil)
	c.Assert(err, IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", srvFile},
		{"reload-or-restart", srvFile},
	})

	s.sysdLog = nil
	flags.Reload = false
	c.Assert(wrappers.RestartServices(info.Services(), nil, flags, progress.Null, s.perfTimings), IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", srvFile},
		{"stop", srvFile},
		{"show", "--property=ActiveState", srvFile},
		{"start", srvFile},
	})

	s.sysdLog = nil
	c.Assert(wrappers.RestartServices(info.Services(), nil, nil, progress.Null, s.perfTimings), IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", srvFile},
		{"stop", srvFile},
		{"show", "--property=ActiveState", srvFile},
		{"start", srvFile},
	})
}

func (s *servicesTestSuite) TestRestartInDifferentStates(c *C) {
	const manyServicesYaml = `name: test-snap
version: 1.0
apps:
  svc1:
    command: bin/foo
    daemon: simple
  svc2:
    command: bin/foo
    daemon: simple
  svc3:
    command: bin/foo
    daemon: simple
  svc4:
    command: bin/foo
    daemon: simple
`
	srvFile1 := "snap.test-snap.svc1.service"
	srvFile2 := "snap.test-snap.svc2.service"
	srvFile3 := "snap.test-snap.svc3.service"
	srvFile4 := "snap.test-snap.svc4.service"

	info := snaptest.MockSnap(c, manyServicesYaml, &snap.SideInfo{Revision: snap.R(1)})

	r := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		s.sysdLog = append(s.sysdLog, cmd)
		states := map[string]systemdtest.ServiceState{
			srvFile1: {ActiveState: "active", UnitFileState: "enabled"},
			srvFile2: {ActiveState: "inactive", UnitFileState: "enabled"},
			srvFile3: {ActiveState: "active", UnitFileState: "disabled"},
			srvFile4: {ActiveState: "inactive", UnitFileState: "disabled"},
		}
		if out := systemdtest.HandleMockAllUnitsActiveOutput(cmd, states); out != nil {
			return out, nil
		}
		return []byte("ActiveState=inactive\n"), nil
	})
	defer r()

	err := wrappers.AddSnapServices(info, nil, progress.Null)
	c.Assert(err, IsNil)

	s.sysdLog = nil
	services := info.Services()
	sort.Sort(snap.AppInfoBySnapApp(services))
	c.Assert(wrappers.RestartServices(services, nil, nil, progress.Null, s.perfTimings), IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload",
			srvFile1, srvFile2, srvFile3, srvFile4},
		{"stop", srvFile1},
		{"show", "--property=ActiveState", srvFile1},
		{"start", srvFile1},
		{"stop", srvFile3},
		{"show", "--property=ActiveState", srvFile3},
		{"start", srvFile3},
	})

	// Verify that explicitly mentioning a service causes it to restart,
	// regardless of its state
	s.sysdLog = nil
	c.Assert(wrappers.RestartServices(services, []string{srvFile2}, nil, progress.Null, s.perfTimings), IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload",
			srvFile1, srvFile2, srvFile3, srvFile4},
		{"stop", srvFile1},
		{"show", "--property=ActiveState", srvFile1},
		{"start", srvFile1},
		{"stop", srvFile2},
		{"show", "--property=ActiveState", srvFile2},
		{"start", srvFile2},
		{"stop", srvFile3},
		{"show", "--property=ActiveState", srvFile3},
		{"start", srvFile3},
	})
}

func (s *servicesTestSuite) TestStopAndDisableServices(c *C) {
	info := snaptest.MockSnap(c, packageHelloNoSrv+`
 svc1:
  daemon: simple
`, &snap.SideInfo{Revision: snap.R(12)})
	svcFile := "snap.hello-snap.svc1.service"

	err := wrappers.AddSnapServices(info, nil, progress.Null)
	c.Assert(err, IsNil)

	s.sysdLog = nil
	flags := &wrappers.StopServicesFlags{Disable: true}
	err = wrappers.StopServices(info.Services(), flags, "", progress.Null, s.perfTimings)
	c.Assert(err, IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"stop", svcFile},
		{"show", "--property=ActiveState", svcFile},
		{"disable", svcFile},
	})
}
