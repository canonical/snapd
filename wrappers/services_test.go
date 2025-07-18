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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/dirs/dirstest"
	"github.com/snapcore/snapd/gadget/quantity"

	// imported to ensure actual interfaces are defined (in production this is guaranteed by ifacestate)
	_ "github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/user"
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
	dirstest.MustMockCanonicalSnapMountDir(s.tempdir)
	dirs.SetRootDir(s.tempdir)

	s.systemctlRestorer = systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		s.sysdLog = append(s.sysdLog, cmd)
		return []byte("ActiveState=inactive\n"), nil
	})
	s.delaysRestorer = systemd.MockStopDelays(2*time.Millisecond, 4*time.Millisecond)
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

// addSnapServices adds service units for the applications from the snap which
// are services. The services do not get enabled or started.
func (s *servicesTestSuite) addSnapServices(snapInfo *snap.Info, preseeding bool) error {
	m := map[*snap.Info]*wrappers.SnapServiceOptions{
		snapInfo: nil,
	}
	ensureOpts := &wrappers.EnsureSnapServicesOptions{
		Preseeding: preseeding,
	}
	return wrappers.EnsureSnapServices(m, ensureOpts, nil, progress.Null)
}

func (s *servicesTestSuite) TestAddSnapServicesAndRemove(c *C) {
	info := snaptest.MockSnap(c, packageHello, &snap.SideInfo{Revision: snap.R(12)})
	svcFile := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.hello-snap.svc1.service")

	err := s.addSnapServices(info, false)
	c.Assert(err, IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"daemon-reload"},
	})

	s.sysdLog = nil

	opts := &wrappers.StartServicesOptions{Enable: true}
	err = wrappers.StartServices(info.Services(), nil, opts, progress.Null, s.perfTimings)
	c.Assert(err, IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"--no-reload", "enable", filepath.Base(svcFile)},
		{"daemon-reload"},
		{"start", filepath.Base(svcFile)},
	})

	dir := dirs.StripRootDir(filepath.Join(dirs.SnapMountDir, "hello-snap", "12.mount"))
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
WorkingDirectory=/var/snap/hello-snap/12
ExecStop=/usr/bin/snap run --command=stop hello-snap.svc1
ExecStopPost=/usr/bin/snap run --command=post-stop hello-snap.svc1
TimeoutStopSec=30
Type=forking

[Install]
WantedBy=multi-user.target
`,
		systemd.EscapeUnitNamePath(dir),
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
		{"--no-reload", "disable", filepath.Base(svcFile)},
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
	svcFile := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.hello-snap.svc1.service")

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

	dir := dirs.StripRootDir(filepath.Join(dirs.SnapMountDir, "hello-snap", "12.mount"))
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
WorkingDirectory=/var/snap/hello-snap/12
ExecStop=/usr/bin/snap run --command=stop hello-snap.svc1
ExecStopPost=/usr/bin/snap run --command=post-stop hello-snap.svc1
TimeoutStopSec=30
Type=forking

[Install]
WantedBy=multi-user.target
`,
		systemd.EscapeUnitNamePath(dir),
	))
}

func (s *servicesTestSuite) TestEnsureSnapServicesWithQuotas(c *C) {
	info := snaptest.MockSnap(c, packageHello, &snap.SideInfo{Revision: snap.R(12)})
	svcFile := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.hello-snap.svc1.service")

	// set up arbitrary quotas for the group to test they get written correctly to the slice
	resourceLimits := quota.NewResourcesBuilder().
		WithMemoryLimit(quantity.SizeGiB).
		WithCPUCount(2).
		WithCPUPercentage(50).
		WithCPUSet([]int{0, 1}).
		WithThreadLimit(32).
		Build()
	grp, err := quota.NewGroup("foogroup", resourceLimits)
	c.Assert(err, IsNil)

	m := map[*snap.Info]*wrappers.SnapServiceOptions{
		info: {QuotaGroup: grp},
	}

	dir := dirs.StripRootDir(filepath.Join(dirs.SnapMountDir, "hello-snap", "12.mount"))
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
WorkingDirectory=/var/snap/hello-snap/12
ExecStop=/usr/bin/snap run --command=stop hello-snap.svc1
ExecStopPost=/usr/bin/snap run --command=post-stop hello-snap.svc1
TimeoutStopSec=30
Type=forking
Slice=snap.foogroup.slice

[Install]
WantedBy=multi-user.target
`,
		systemd.EscapeUnitNamePath(dir),
	)

	sliceTempl := `[Unit]
Description=Slice for snap quota group %s
Before=slices.target
X-Snappy=yes

[Slice]
# Always enable cpu accounting, so the following cpu quota options have an effect
CPUAccounting=true
CPUQuota=%[2]d%%
AllowedCPUs=%[3]s

# Always enable memory accounting otherwise the MemoryMax setting does nothing.
MemoryAccounting=true
MemoryMax=%[4]d
# for compatibility with older versions of systemd
MemoryLimit=%[4]d

# Always enable task accounting in order to be able to count the processes/
# threads, etc for a slice
TasksAccounting=true
TasksMax=%[5]d
`

	allowedCpusValue := strutil.IntsToCommaSeparated(resourceLimits.CPUSet.CPUs)
	sliceContent := fmt.Sprintf(sliceTempl, grp.Name,
		resourceLimits.CPU.Count*resourceLimits.CPU.Percentage,
		allowedCpusValue,
		resourceLimits.Memory.Limit,
		resourceLimits.Threads.Limit)

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

func (s *servicesTestSuite) TestEnsureSnapServicesWithZeroCpuCountQuotas(c *C) {
	// Kind of a special case, if the cpu count is zero it needs to automatically scale
	// at the moment of writing the service file to the current number of cpu cores
	info := snaptest.MockSnap(c, packageHello, &snap.SideInfo{Revision: snap.R(12)})
	svcFile := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.hello-snap.svc1.service")

	// set up arbitrary quotas for the group to test they get written correctly to the slice
	resourceLimits := quota.NewResourcesBuilder().
		WithCPUPercentage(50).
		Build()
	grp, err := quota.NewGroup("foogroup", resourceLimits)
	c.Assert(err, IsNil)

	m := map[*snap.Info]*wrappers.SnapServiceOptions{
		info: {QuotaGroup: grp},
	}

	dir := dirs.StripRootDir(filepath.Join(dirs.SnapMountDir, "hello-snap", "12.mount"))
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
WorkingDirectory=/var/snap/hello-snap/12
ExecStop=/usr/bin/snap run --command=stop hello-snap.svc1
ExecStopPost=/usr/bin/snap run --command=post-stop hello-snap.svc1
TimeoutStopSec=30
Type=forking
Slice=snap.foogroup.slice

[Install]
WantedBy=multi-user.target
`,
		systemd.EscapeUnitNamePath(dir),
	)

	sliceTempl := `[Unit]
Description=Slice for snap quota group %s
Before=slices.target
X-Snappy=yes

[Slice]
# Always enable cpu accounting, so the following cpu quota options have an effect
CPUAccounting=true
CPUQuota=%[2]d%%

# Always enable memory accounting otherwise the MemoryMax setting does nothing.
MemoryAccounting=true
# Always enable task accounting in order to be able to count the processes/
# threads, etc for a slice
TasksAccounting=true
`
	// The reason we are not mocking the cpu count here is because we are relying
	// on the real code to produce the slice file content, and it will always use
	// the current number of cores, so we also use the current number of cores of
	// the test runner here.
	sliceContent := fmt.Sprintf(sliceTempl, grp.Name,
		runtime.NumCPU()*resourceLimits.CPU.Percentage)

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

func (s *servicesTestSuite) TestEnsureSnapServicesWithZeroCpuCountAndCpuSetQuotas(c *C) {
	// Another special case, if the cpu count is zero it needs to automatically scale as the
	// previous test, but only up the maximum allowed provided in the cpu-set. So in this test
	// we provide only 1 allowed CPU, which means that the percentage that will be written is 50%
	// and not 200% or how many cores that runs this test!
	info := snaptest.MockSnap(c, packageHello, &snap.SideInfo{Revision: snap.R(12)})
	svcFile := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.hello-snap.svc1.service")

	// set up arbitrary quotas for the group to test they get written correctly to the slice
	resourceLimits := quota.NewResourcesBuilder().
		WithCPUPercentage(50).
		WithCPUSet([]int{0}).
		Build()
	grp, err := quota.NewGroup("foogroup", resourceLimits)
	c.Assert(err, IsNil)

	m := map[*snap.Info]*wrappers.SnapServiceOptions{
		info: {QuotaGroup: grp},
	}

	dir := dirs.StripRootDir(filepath.Join(dirs.SnapMountDir, "hello-snap", "12.mount"))
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
WorkingDirectory=/var/snap/hello-snap/12
ExecStop=/usr/bin/snap run --command=stop hello-snap.svc1
ExecStopPost=/usr/bin/snap run --command=post-stop hello-snap.svc1
TimeoutStopSec=30
Type=forking
Slice=snap.foogroup.slice

[Install]
WantedBy=multi-user.target
`,
		systemd.EscapeUnitNamePath(dir),
	)

	sliceTempl := `[Unit]
Description=Slice for snap quota group %s
Before=slices.target
X-Snappy=yes

[Slice]
# Always enable cpu accounting, so the following cpu quota options have an effect
CPUAccounting=true
CPUQuota=50%%
AllowedCPUs=0

# Always enable memory accounting otherwise the MemoryMax setting does nothing.
MemoryAccounting=true
# Always enable task accounting in order to be able to count the processes/
# threads, etc for a slice
TasksAccounting=true
`

	sliceContent := fmt.Sprintf(sliceTempl, grp.Name)

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

func (s *servicesTestSuite) TestEnsureSnapServicesWithJournalNamespaceOnly(c *C) {
	// Ensure that the journald.conf file is correctly written
	info := snaptest.MockSnap(c, packageHello, &snap.SideInfo{Revision: snap.R(12)})
	svcFile := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.hello-snap.svc1.service")

	// set up arbitrary quotas for the group to test they get written correctly to the slice
	resourceLimits := quota.NewResourcesBuilder().
		WithJournalNamespace().
		Build()
	grp, err := quota.NewGroup("foogroup", resourceLimits)
	c.Assert(err, IsNil)

	m := map[*snap.Info]*wrappers.SnapServiceOptions{
		info: {QuotaGroup: grp},
	}

	dir := dirs.StripRootDir(filepath.Join(dirs.SnapMountDir, "hello-snap", "12.mount"))
	svcContent := fmt.Sprintf(`[Unit]
# Auto-generated, DO NOT EDIT
Description=Service for snap application hello-snap.svc1
Requires=systemd-journald@snap-foogroup.socket %[1]s
Wants=network.target
After=%[1]s network.target systemd-journald@snap-foogroup.socket snapd.apparmor.service
X-Snappy=yes

[Service]
EnvironmentFile=-/etc/environment
Environment=SNAPD_LOG_NAMESPACE=snap-foogroup
ExecStart=/usr/bin/snap run hello-snap.svc1
SyslogIdentifier=hello-snap.svc1
Restart=on-failure
WorkingDirectory=/var/snap/hello-snap/12
ExecStop=/usr/bin/snap run --command=stop hello-snap.svc1
ExecStopPost=/usr/bin/snap run --command=post-stop hello-snap.svc1
TimeoutStopSec=30
Type=forking
Slice=snap.foogroup.slice

[Install]
WantedBy=multi-user.target
`,
		systemd.EscapeUnitNamePath(dir),
	)
	jconfTempl := `# Journald configuration for snap quota group %s
[Journal]
Storage=auto
`

	jSvcContent := `[Service]
LogsDirectory=
`

	sliceTempl := `[Unit]
Description=Slice for snap quota group %s
Before=slices.target
X-Snappy=yes

[Slice]
# Always enable cpu accounting, so the following cpu quota options have an effect
CPUAccounting=true

# Always enable memory accounting otherwise the MemoryMax setting does nothing.
MemoryAccounting=true
# Always enable task accounting in order to be able to count the processes/
# threads, etc for a slice
TasksAccounting=true
`

	jconfContent := fmt.Sprintf(jconfTempl, grp.Name)
	sliceContent := fmt.Sprintf(sliceTempl, grp.Name)

	exp := []changesObservation{
		{
			grp:      grp,
			unitType: "journald",
			new:      jconfContent,
			old:      "",
			name:     "foogroup",
		},
		{
			grp:      grp,
			unitType: "service",
			new:      jSvcContent,
			old:      "",
			name:     "foogroup",
		},
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

func (s *servicesTestSuite) TestEnsureSnapServicesWithJournalQuotas(c *C) {
	// Ensure that the journald.conf file is correctly written
	info := snaptest.MockSnap(c, packageHello, &snap.SideInfo{Revision: snap.R(12)})
	svcFile := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.hello-snap.svc1.service")

	// set up arbitrary quotas for the group to test they get written correctly to the slice
	resourceLimits := quota.NewResourcesBuilder().
		WithJournalSize(10*quantity.SizeMiB).
		WithJournalRate(15, 5*time.Second).
		Build()
	grp, err := quota.NewGroup("foogroup", resourceLimits)
	c.Assert(err, IsNil)

	m := map[*snap.Info]*wrappers.SnapServiceOptions{
		info: {QuotaGroup: grp},
	}

	dir := dirs.StripRootDir(filepath.Join(dirs.SnapMountDir, "hello-snap", "12.mount"))
	svcContent := fmt.Sprintf(`[Unit]
# Auto-generated, DO NOT EDIT
Description=Service for snap application hello-snap.svc1
Requires=systemd-journald@snap-foogroup.socket %[1]s
Wants=network.target
After=%[1]s network.target systemd-journald@snap-foogroup.socket snapd.apparmor.service
X-Snappy=yes

[Service]
EnvironmentFile=-/etc/environment
Environment=SNAPD_LOG_NAMESPACE=snap-foogroup
ExecStart=/usr/bin/snap run hello-snap.svc1
SyslogIdentifier=hello-snap.svc1
Restart=on-failure
WorkingDirectory=/var/snap/hello-snap/12
ExecStop=/usr/bin/snap run --command=stop hello-snap.svc1
ExecStopPost=/usr/bin/snap run --command=post-stop hello-snap.svc1
TimeoutStopSec=30
Type=forking
Slice=snap.foogroup.slice

[Install]
WantedBy=multi-user.target
`,
		systemd.EscapeUnitNamePath(dir),
	)
	jconfTempl := `# Journald configuration for snap quota group %s
[Journal]
Storage=auto
SystemMaxUse=10485760
RuntimeMaxUse=10485760
RateLimitIntervalSec=5000000us
RateLimitBurst=15
`

	jSvcContent := `[Service]
LogsDirectory=
`

	sliceTempl := `[Unit]
Description=Slice for snap quota group %s
Before=slices.target
X-Snappy=yes

[Slice]
# Always enable cpu accounting, so the following cpu quota options have an effect
CPUAccounting=true

# Always enable memory accounting otherwise the MemoryMax setting does nothing.
MemoryAccounting=true
# Always enable task accounting in order to be able to count the processes/
# threads, etc for a slice
TasksAccounting=true
`

	jconfContent := fmt.Sprintf(jconfTempl, grp.Name)
	sliceContent := fmt.Sprintf(sliceTempl, grp.Name)

	exp := []changesObservation{
		{
			grp:      grp,
			unitType: "journald",
			new:      jconfContent,
			old:      "",
			name:     "foogroup",
		},
		{
			grp:      grp,
			unitType: "service",
			new:      jSvcContent,
			old:      "",
			name:     "foogroup",
		},
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

func (s *servicesTestSuite) TestEnsureSnapServicesWithJournalQuotaRateAsZero(c *C) {
	// Ensure that the journald.conf file is correctly written
	info := snaptest.MockSnap(c, packageHello, &snap.SideInfo{Revision: snap.R(12)})
	svcFile := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.hello-snap.svc1.service")

	// set up arbitrary quotas for the group to test they get written correctly to the slice
	resourceLimits := quota.NewResourcesBuilder().
		WithJournalRate(0, 0).
		Build()
	grp, err := quota.NewGroup("foogroup", resourceLimits)
	c.Assert(err, IsNil)

	m := map[*snap.Info]*wrappers.SnapServiceOptions{
		info: {QuotaGroup: grp},
	}

	dir := dirs.StripRootDir(filepath.Join(dirs.SnapMountDir, "hello-snap", "12.mount"))
	svcContent := fmt.Sprintf(`[Unit]
# Auto-generated, DO NOT EDIT
Description=Service for snap application hello-snap.svc1
Requires=systemd-journald@snap-foogroup.socket %[1]s
Wants=network.target
After=%[1]s network.target systemd-journald@snap-foogroup.socket snapd.apparmor.service
X-Snappy=yes

[Service]
EnvironmentFile=-/etc/environment
Environment=SNAPD_LOG_NAMESPACE=snap-foogroup
ExecStart=/usr/bin/snap run hello-snap.svc1
SyslogIdentifier=hello-snap.svc1
Restart=on-failure
WorkingDirectory=/var/snap/hello-snap/12
ExecStop=/usr/bin/snap run --command=stop hello-snap.svc1
ExecStopPost=/usr/bin/snap run --command=post-stop hello-snap.svc1
TimeoutStopSec=30
Type=forking
Slice=snap.foogroup.slice

[Install]
WantedBy=multi-user.target
`,
		systemd.EscapeUnitNamePath(dir),
	)
	jconfTempl := `# Journald configuration for snap quota group %s
[Journal]
Storage=auto
RateLimitIntervalSec=0us
RateLimitBurst=0
`

	jSvcContent := `[Service]
LogsDirectory=
`

	sliceTempl := `[Unit]
Description=Slice for snap quota group %s
Before=slices.target
X-Snappy=yes

[Slice]
# Always enable cpu accounting, so the following cpu quota options have an effect
CPUAccounting=true

# Always enable memory accounting otherwise the MemoryMax setting does nothing.
MemoryAccounting=true
# Always enable task accounting in order to be able to count the processes/
# threads, etc for a slice
TasksAccounting=true
`

	jconfContent := fmt.Sprintf(jconfTempl, grp.Name)
	sliceContent := fmt.Sprintf(sliceTempl, grp.Name)

	exp := []changesObservation{
		{
			grp:      grp,
			unitType: "journald",
			new:      jconfContent,
			old:      "",
			name:     "foogroup",
		},
		{
			grp:      grp,
			unitType: "service",
			new:      jSvcContent,
			old:      "",
			name:     "foogroup",
		},
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

const testSnapServicesYaml = `name: hello-snap
version: 1
apps:
 svc1:
   command: bin/hello
   daemon: simple
 svc2:
   command: bin/world
   daemon: simple
`

func (s *servicesTestSuite) TestEnsureSnapServicesWithSnapServices(c *C) {
	// Test ensures that if a snap has services in a sub-group, the sub-group
	// slice is correctly applied to the service unit for the snap service. We should
	// see two slices created (one for root group which has the snap, and one for the
	// sub-group which has the service).
	// Furthermore we should see the service unit file for hello-snap.svc2 refer to the
	// sub-group slice, and not the slice for the primary group.
	info := snaptest.MockSnap(c, testSnapServicesYaml, &snap.SideInfo{Revision: snap.R(12)})
	svc1File := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.hello-snap.svc1.service")
	svc2File := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.hello-snap.svc2.service")

	// setup quotas for the parent, including a journal quota to verify service sub-groups
	// correctly inherit the journal namespace, even without having one directly specified.
	grp, err := quota.NewGroup("my-root", quota.NewResourcesBuilder().
		WithMemoryLimit(quantity.SizeGiB).
		WithJournalNamespace().
		WithCPUPercentage(50).
		WithCPUCount(1).
		Build())
	c.Assert(err, IsNil)

	grp.Snaps = []string{"hello-snap"}

	// setup basic quota for the service sub-group
	sub, err := grp.NewSubGroup("my-sub", quota.NewResourcesBuilder().
		WithMemoryLimit(quantity.SizeGiB/2).
		Build())
	c.Assert(err, IsNil)

	sub.Services = []string{"hello-snap.svc2"}

	m := map[*snap.Info]*wrappers.SnapServiceOptions{
		info: {QuotaGroup: grp},
	}

	dir := dirs.StripRootDir(filepath.Join(dirs.SnapMountDir, "hello-snap", "12.mount"))
	svc1Content := fmt.Sprintf(`[Unit]
# Auto-generated, DO NOT EDIT
Description=Service for snap application hello-snap.svc1
Requires=systemd-journald@snap-my-root.socket %[1]s
Wants=network.target
After=%[1]s network.target systemd-journald@snap-my-root.socket snapd.apparmor.service
X-Snappy=yes

[Service]
EnvironmentFile=-/etc/environment
Environment=SNAPD_LOG_NAMESPACE=snap-my-root
ExecStart=/usr/bin/snap run hello-snap.svc1
SyslogIdentifier=hello-snap.svc1
Restart=on-failure
WorkingDirectory=/var/snap/hello-snap/12
TimeoutStopSec=30
Type=simple
Slice=snap.%[2]s.slice

[Install]
WantedBy=multi-user.target
`,
		systemd.EscapeUnitNamePath(dir),
		systemd.EscapeUnitNamePath("my-root"),
	)
	svc2Content := fmt.Sprintf(`[Unit]
# Auto-generated, DO NOT EDIT
Description=Service for snap application hello-snap.svc2
Requires=systemd-journald@snap-my-root.socket %[1]s
Wants=network.target
After=%[1]s network.target systemd-journald@snap-my-root.socket snapd.apparmor.service
X-Snappy=yes

[Service]
EnvironmentFile=-/etc/environment
Environment=SNAPD_LOG_NAMESPACE=snap-my-root
ExecStart=/usr/bin/snap run hello-snap.svc2
SyslogIdentifier=hello-snap.svc2
Restart=on-failure
WorkingDirectory=/var/snap/hello-snap/12
TimeoutStopSec=30
Type=simple
Slice=snap.%[2]s.slice

[Install]
WantedBy=multi-user.target
`,
		systemd.EscapeUnitNamePath(dir),
		systemd.EscapeUnitNamePath("my-root")+"-"+systemd.EscapeUnitNamePath("my-sub"),
	)
	jSvcContent := `[Service]
LogsDirectory=
`

	rootSliceTempl := `[Unit]
Description=Slice for snap quota group %s
Before=slices.target
X-Snappy=yes

[Slice]
# Always enable cpu accounting, so the following cpu quota options have an effect
CPUAccounting=true
CPUQuota=50%%

# Always enable memory accounting otherwise the MemoryMax setting does nothing.
MemoryAccounting=true
MemoryMax=1073741824
# for compatibility with older versions of systemd
MemoryLimit=1073741824

# Always enable task accounting in order to be able to count the processes/
# threads, etc for a slice
TasksAccounting=true
`

	subSliceTempl := `[Unit]
Description=Slice for snap quota group %s
Before=slices.target
X-Snappy=yes

[Slice]
# Always enable cpu accounting, so the following cpu quota options have an effect
CPUAccounting=true

# Always enable memory accounting otherwise the MemoryMax setting does nothing.
MemoryAccounting=true
MemoryMax=536870912
# for compatibility with older versions of systemd
MemoryLimit=536870912

# Always enable task accounting in order to be able to count the processes/
# threads, etc for a slice
TasksAccounting=true
`

	jconfTempl := `# Journald configuration for snap quota group %s
[Journal]
Storage=auto
`

	rootSliceContent := fmt.Sprintf(rootSliceTempl, grp.Name)
	subSliceContent := fmt.Sprintf(subSliceTempl, sub.Name)
	jconfContent := fmt.Sprintf(jconfTempl, grp.Name)

	exp := []changesObservation{
		{
			grp:      grp,
			unitType: "journald",
			new:      jconfContent,
			old:      "",
			name:     "my-root",
		},
		{
			grp:      grp,
			unitType: "service",
			new:      jSvcContent,
			old:      "",
			name:     "my-root",
		},
		{
			snapName: "hello-snap",
			unitType: "service",
			name:     "svc1",
			old:      "",
			new:      svc1Content,
		},
		{
			snapName: "hello-snap",
			unitType: "service",
			name:     "svc2",
			old:      "",
			new:      svc2Content,
		},
		{
			grp:      grp,
			unitType: "slice",
			new:      rootSliceContent,
			old:      "",
			name:     "my-root",
		},
		{
			grp:      sub,
			unitType: "slice",
			new:      subSliceContent,
			old:      "",
			name:     "my-sub",
		},
	}
	r, observe := expChangeObserver(c, exp)
	defer r()

	err = wrappers.EnsureSnapServices(m, nil, observe, progress.Null)
	c.Assert(err, IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"daemon-reload"},
	})

	c.Assert(svc1File, testutil.FileEquals, svc1Content)
	c.Assert(svc2File, testutil.FileEquals, svc2Content)
}

func (s *servicesTestSuite) TestEnsureSnapServicesWithIncludeServices(c *C) {
	// Test ensures that the IncludeServices member is working as expected. We have a snap
	// that contains multiple services, however we only include svc2 in the IncludeServices
	// option to EnsureSnapServices. Thus what we will observe happen is that only the service
	// file for svc2 will be written.
	info := snaptest.MockSnap(c, testSnapServicesYaml, &snap.SideInfo{Revision: snap.R(12)})
	svc2File := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.hello-snap.svc2.service")

	// set up arbitrary quotas for the group to test they get written correctly to the slice
	grp, err := quota.NewGroup("my-root", quota.NewResourcesBuilder().
		WithMemoryLimit(quantity.SizeGiB).
		WithJournalNamespace().
		WithCPUPercentage(50).
		WithCPUCount(1).
		Build())
	c.Assert(err, IsNil)

	grp.Snaps = []string{"hello-snap"}

	sub, err := grp.NewSubGroup("my-sub", quota.NewResourcesBuilder().
		WithMemoryLimit(quantity.SizeGiB/2).
		Build())
	c.Assert(err, IsNil)

	sub.Services = []string{"hello-snap.svc2"}

	m := map[*snap.Info]*wrappers.SnapServiceOptions{
		info: {QuotaGroup: grp},
	}

	dir := dirs.StripRootDir(filepath.Join(dirs.SnapMountDir, "hello-snap", "12.mount"))
	svc2Content := fmt.Sprintf(`[Unit]
# Auto-generated, DO NOT EDIT
Description=Service for snap application hello-snap.svc2
Requires=systemd-journald@snap-my-root.socket %[1]s
Wants=network.target
After=%[1]s network.target systemd-journald@snap-my-root.socket snapd.apparmor.service
X-Snappy=yes

[Service]
EnvironmentFile=-/etc/environment
Environment=SNAPD_LOG_NAMESPACE=snap-my-root
ExecStart=/usr/bin/snap run hello-snap.svc2
SyslogIdentifier=hello-snap.svc2
Restart=on-failure
WorkingDirectory=/var/snap/hello-snap/12
TimeoutStopSec=30
Type=simple
Slice=snap.%[2]s.slice

[Install]
WantedBy=multi-user.target
`,
		systemd.EscapeUnitNamePath(dir),
		systemd.EscapeUnitNamePath("my-root")+"-"+systemd.EscapeUnitNamePath("my-sub"),
	)
	jSvcContent := `[Service]
LogsDirectory=
`

	rootSliceTempl := `[Unit]
Description=Slice for snap quota group %s
Before=slices.target
X-Snappy=yes

[Slice]
# Always enable cpu accounting, so the following cpu quota options have an effect
CPUAccounting=true
CPUQuota=50%%

# Always enable memory accounting otherwise the MemoryMax setting does nothing.
MemoryAccounting=true
MemoryMax=1073741824
# for compatibility with older versions of systemd
MemoryLimit=1073741824

# Always enable task accounting in order to be able to count the processes/
# threads, etc for a slice
TasksAccounting=true
`

	subSliceTempl := `[Unit]
Description=Slice for snap quota group %s
Before=slices.target
X-Snappy=yes

[Slice]
# Always enable cpu accounting, so the following cpu quota options have an effect
CPUAccounting=true

# Always enable memory accounting otherwise the MemoryMax setting does nothing.
MemoryAccounting=true
MemoryMax=536870912
# for compatibility with older versions of systemd
MemoryLimit=536870912

# Always enable task accounting in order to be able to count the processes/
# threads, etc for a slice
TasksAccounting=true
`

	jconfTempl := `# Journald configuration for snap quota group %s
[Journal]
Storage=auto
`

	rootSliceContent := fmt.Sprintf(rootSliceTempl, grp.Name)
	subSliceContent := fmt.Sprintf(subSliceTempl, sub.Name)
	jconfContent := fmt.Sprintf(jconfTempl, grp.Name)

	exp := []changesObservation{
		{
			grp:      grp,
			unitType: "journald",
			new:      jconfContent,
			old:      "",
			name:     "my-root",
		},
		{
			grp:      grp,
			unitType: "service",
			new:      jSvcContent,
			old:      "",
			name:     "my-root",
		},
		{
			snapName: "hello-snap",
			unitType: "service",
			name:     "svc2",
			old:      "",
			new:      svc2Content,
		},
		{
			grp:      grp,
			unitType: "slice",
			new:      rootSliceContent,
			old:      "",
			name:     "my-root",
		},
		{
			grp:      sub,
			unitType: "slice",
			new:      subSliceContent,
			old:      "",
			name:     "my-sub",
		},
	}
	r, observe := expChangeObserver(c, exp)
	defer r()

	err = wrappers.EnsureSnapServices(m, &wrappers.EnsureSnapServicesOptions{
		IncludeServices: []string{"hello-snap.svc2"},
	}, observe, progress.Null)
	c.Assert(err, IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"daemon-reload"},
	})

	c.Assert(svc2File, testutil.FileEquals, svc2Content)
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
	svcFile := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.hello-snap.svc1.service")

	memLimit1 := quantity.SizeGiB
	memLimit2 := quantity.SizeGiB * 2

	// write both the unit file and a slice with a different memory limit
	// setting than will be provided below
	sliceTempl := `[Unit]
Description=Slice for snap quota group %s
Before=slices.target
X-Snappy=yes

[Slice]
# Always enable cpu accounting, so the following cpu quota options have an effect
CPUAccounting=true

# Always enable memory accounting otherwise the MemoryMax setting does nothing.
MemoryAccounting=true
MemoryMax=%[2]s
# for compatibility with older versions of systemd
MemoryLimit=%[2]s

# Always enable task accounting in order to be able to count the processes/
# threads, etc for a slice
TasksAccounting=true
`
	sliceFile := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.foogroup.slice")

	dir := dirs.StripRootDir(filepath.Join(dirs.SnapMountDir, "hello-snap", "12.mount"))
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
WorkingDirectory=/var/snap/hello-snap/12
ExecStop=/usr/bin/snap run --command=stop hello-snap.svc1
ExecStopPost=/usr/bin/snap run --command=post-stop hello-snap.svc1
TimeoutStopSec=30
Type=forking
Slice=snap.foogroup.slice

[Install]
WantedBy=multi-user.target
`,
		systemd.EscapeUnitNamePath(dir),
	)

	err := os.MkdirAll(filepath.Dir(sliceFile), 0755)
	c.Assert(err, IsNil)

	oldContent := fmt.Sprintf(sliceTempl, "foogroup", memLimit1.String())
	err = os.WriteFile(sliceFile, []byte(oldContent), 0644)
	c.Assert(err, IsNil)

	err = os.WriteFile(svcFile, []byte(svcContent), 0644)
	c.Assert(err, IsNil)

	// use new memory limit
	resourceLimits := quota.NewResourcesBuilder().WithMemoryLimit(memLimit2).Build()
	grp, err := quota.NewGroup("foogroup", resourceLimits)
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
	svcFile := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.hello-snap.svc1.service")

	memLimit := quantity.SizeGiB
	taskLimit := 32 // arbitrarily chosen

	// write both the unit file and a slice before running the ensure
	sliceTempl := `[Unit]
Description=Slice for snap quota group %s
Before=slices.target
X-Snappy=yes

[Slice]
# Always enable cpu accounting, so the following cpu quota options have an effect
CPUAccounting=true

# Always enable memory accounting otherwise the MemoryMax setting does nothing.
MemoryAccounting=true
MemoryMax=%[2]s
# for compatibility with older versions of systemd
MemoryLimit=%[2]s

# Always enable task accounting in order to be able to count the processes/
# threads, etc for a slice
TasksAccounting=true
TasksMax=%[3]d
`
	sliceFile := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.foogroup.slice")

	dir := dirs.StripRootDir(filepath.Join(dirs.SnapMountDir, "hello-snap", "12.mount"))
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
WorkingDirectory=/var/snap/hello-snap/12
ExecStop=/usr/bin/snap run --command=stop hello-snap.svc1
ExecStopPost=/usr/bin/snap run --command=post-stop hello-snap.svc1
TimeoutStopSec=30
Type=forking
Slice=snap.foogroup.slice

[Install]
WantedBy=multi-user.target
`,
		systemd.EscapeUnitNamePath(dir),
	)

	err := os.MkdirAll(filepath.Dir(sliceFile), 0755)
	c.Assert(err, IsNil)

	err = os.WriteFile(sliceFile, []byte(fmt.Sprintf(sliceTempl, "foogroup", memLimit.String(), taskLimit)), 0644)
	c.Assert(err, IsNil)

	err = os.WriteFile(svcFile, []byte(svcContent), 0644)
	c.Assert(err, IsNil)

	resourceLimits := quota.NewResourcesBuilder().WithMemoryLimit(memLimit).WithThreadLimit(taskLimit).Build()
	grp, err := quota.NewGroup("foogroup", resourceLimits)
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

	c.Assert(sliceFile, testutil.FileEquals, fmt.Sprintf(sliceTempl, "foogroup", memLimit.String(), taskLimit))
}

func (s *servicesTestSuite) TestRemoveQuotaGroup(c *C) {
	// create the group
	resourceLimits := quota.NewResourcesBuilder().WithMemoryLimit(650 * quantity.SizeKiB).Build()
	grp, err := quota.NewGroup("foogroup", resourceLimits)
	c.Assert(err, IsNil)

	sliceFile := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.foogroup.slice")
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
# Always enable cpu accounting, so the following cpu quota options have an effect
CPUAccounting=true

# Always enable memory accounting otherwise the MemoryMax setting does nothing.
MemoryAccounting=true
MemoryMax=1024
# for compatibility with older versions of systemd
MemoryLimit=1024
`

	err = os.MkdirAll(filepath.Dir(sliceFile), 0755)
	c.Assert(err, IsNil)

	err = os.WriteFile(sliceFile, []byte(fmt.Sprintf(sliceTempl, "foogroup")), 0644)
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
	svcFile1 := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.hello-snap.svc1.service")
	svcFile2 := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.hello-other-snap.svc1.service")

	var err error
	resourceLimits := quota.NewResourcesBuilder().
		WithMemoryLimit(quantity.SizeGiB).
		WithCPUCount(4).
		WithCPUPercentage(25).
		Build()
	// make a root quota group and add the first snap to it
	grp, err := quota.NewGroup("foogroup", resourceLimits)
	c.Assert(err, IsNil)

	// the second group is a sub-group with the same limit, but is for the
	// second snap
	subgrp, err := grp.NewSubGroup("subgroup", resourceLimits)
	c.Assert(err, IsNil)

	sliceFile := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.foogroup.slice")
	subSliceFile := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.foogroup-subgroup.slice")

	m := map[*snap.Info]*wrappers.SnapServiceOptions{
		info1: {QuotaGroup: grp},
		info2: {QuotaGroup: subgrp},
	}

	sliceTempl := `[Unit]
Description=Slice for snap quota group %s
Before=slices.target
X-Snappy=yes

[Slice]
# Always enable cpu accounting, so the following cpu quota options have an effect
CPUAccounting=true
CPUQuota=%[2]d%%

# Always enable memory accounting otherwise the MemoryMax setting does nothing.
MemoryAccounting=true
MemoryMax=%[3]d
# for compatibility with older versions of systemd
MemoryLimit=%[3]d

# Always enable task accounting in order to be able to count the processes/
# threads, etc for a slice
TasksAccounting=true
`

	sliceContent := fmt.Sprintf(sliceTempl, "foogroup", resourceLimits.CPU.Count*resourceLimits.CPU.Percentage, resourceLimits.Memory.Limit)
	subSliceContent := fmt.Sprintf(sliceTempl, "subgroup", resourceLimits.CPU.Count*resourceLimits.CPU.Percentage, resourceLimits.Memory.Limit)

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
WorkingDirectory=/var/snap/%[1]s/12
ExecStop=/usr/bin/snap run --command=stop %[1]s.svc1
ExecStopPost=/usr/bin/snap run --command=post-stop %[1]s.svc1
TimeoutStopSec=30
Type=forking
Slice=%[3]s

[Install]
WantedBy=multi-user.target
`

	dir1 := dirs.StripRootDir(filepath.Join(dirs.SnapMountDir, "hello-snap", "12.mount"))
	dir2 := dirs.StripRootDir(filepath.Join(dirs.SnapMountDir, "hello-other-snap", "12.mount"))

	helloSnapContent := fmt.Sprintf(svcTemplate,
		"hello-snap",
		systemd.EscapeUnitNamePath(dir1),
		"snap.foogroup.slice",
	)

	helloOtherSnapContent := fmt.Sprintf(svcTemplate,
		"hello-other-snap",
		systemd.EscapeUnitNamePath(dir2),
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
	svcFile1 := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.hello-snap.svc1.service")

	var err error
	resourceLimits := quota.NewResourcesBuilder().
		WithMemoryLimit(quantity.SizeGiB).
		WithCPUSet([]int{0}).
		Build()
	// make a root quota group without any snaps in it
	grp, err := quota.NewGroup("foogroup", resourceLimits)
	c.Assert(err, IsNil)

	// the second group is a sub-group with the same limit, but it is the one
	// with the snap in it
	subgrp, err := grp.NewSubGroup("subgroup", resourceLimits)
	c.Assert(err, IsNil)

	sliceFile := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.foogroup.slice")
	subSliceFile := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.foogroup-subgroup.slice")

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
WorkingDirectory=/var/snap/%[1]s/12
ExecStop=/usr/bin/snap run --command=stop %[1]s.svc1
ExecStopPost=/usr/bin/snap run --command=post-stop %[1]s.svc1
TimeoutStopSec=30
Type=forking
Slice=%[3]s

[Install]
WantedBy=multi-user.target
`

	dir1 := dirs.StripRootDir(filepath.Join(dirs.SnapMountDir, "hello-snap", "12.mount"))

	c.Assert(svcFile1, testutil.FileEquals, fmt.Sprintf(svcTemplate,
		"hello-snap",
		systemd.EscapeUnitNamePath(dir1),
		"snap.foogroup-subgroup.slice",
	))

	// check that both the parent and sub-group slice units were generated
	templ := `[Unit]
Description=Slice for snap quota group %s
Before=slices.target
X-Snappy=yes

[Slice]
# Always enable cpu accounting, so the following cpu quota options have an effect
CPUAccounting=true
AllowedCPUs=%[2]s

# Always enable memory accounting otherwise the MemoryMax setting does nothing.
MemoryAccounting=true
MemoryMax=%[3]d
# for compatibility with older versions of systemd
MemoryLimit=%[3]d

# Always enable task accounting in order to be able to count the processes/
# threads, etc for a slice
TasksAccounting=true
`

	allowedCpusValue := strutil.IntsToCommaSeparated(resourceLimits.CPUSet.CPUs)
	c.Assert(sliceFile, testutil.FileEquals, fmt.Sprintf(templ, "foogroup", allowedCpusValue, resourceLimits.Memory.Limit))
	c.Assert(subSliceFile, testutil.FileEquals, fmt.Sprintf(templ, "subgroup", allowedCpusValue, resourceLimits.Memory.Limit))
}

func (s *servicesTestSuite) TestEnsureSnapServiceEnsureError(c *C) {
	info := snaptest.MockSnap(c, packageHello, &snap.SideInfo{Revision: snap.R(12)})
	svcFileDir := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system")

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
	svcFile := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.hello-snap.svc1.service")

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

	dir := dirs.StripRootDir(filepath.Join(dirs.SnapMountDir, "hello-snap", "12.mount"))
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
WorkingDirectory=/var/snap/hello-snap/12
ExecStop=/usr/bin/snap run --command=stop hello-snap.svc1
ExecStopPost=/usr/bin/snap run --command=post-stop hello-snap.svc1
TimeoutStopSec=30
Type=forking

[Install]
WantedBy=multi-user.target
`,
		systemd.EscapeUnitNamePath(dir),
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
	svcFile1 := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.hello-snap.svc1.service")
	svcFile2 := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.hello-other-snap.svc1.service")

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
WorkingDirectory=/var/snap/%[1]s/12
ExecStop=/usr/bin/snap run --command=stop %[1]s.svc1
ExecStopPost=/usr/bin/snap run --command=post-stop %[1]s.svc1
TimeoutStopSec=30
Type=forking
%[3]s
[Install]
WantedBy=multi-user.target
`

	dir1 := dirs.StripRootDir(filepath.Join(dirs.SnapMountDir, "hello-snap", "12.mount"))
	dir2 := dirs.StripRootDir(filepath.Join(dirs.SnapMountDir, "hello-other-snap", "12.mount"))

	c.Assert(svcFile1, testutil.FileEquals, fmt.Sprintf(template,
		"hello-snap",
		systemd.EscapeUnitNamePath(dir1),
		"OOMScoreAdjust=-899\n", // VitalityRank in effect
	))

	c.Assert(svcFile2, testutil.FileEquals, fmt.Sprintf(template,
		"hello-other-snap",
		systemd.EscapeUnitNamePath(dir2),
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
	svc1File := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.hello-snap.svc1.service")
	svc2File := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.hello-snap.svc2.service")

	dir := dirs.StripRootDir(filepath.Join(dirs.SnapMountDir, "hello-snap", "12.mount"))
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
WorkingDirectory=/var/snap/hello-snap/12
ExecStop=/usr/bin/snap run --command=stop hello-snap.%[1]s
ExecStopPost=/usr/bin/snap run --command=post-stop hello-snap.%[1]s
TimeoutStopSec=30
Type=forking
%[3]s
[Install]
WantedBy=multi-user.target
`
	svc1Content := fmt.Sprintf(template,
		"svc1",
		systemd.EscapeUnitNamePath(dir),
		"",
	)

	err := os.MkdirAll(filepath.Dir(svc1File), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(svc1File, []byte(svc1Content), 0644)
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
		"OOMScoreAdjust=-899\n",
	)
	c.Assert(svc2File, testutil.FileEquals, svc2New)

	// and svc1 was changed as well
	svc1New := fmt.Sprintf(template,
		"svc1",
		systemd.EscapeUnitNamePath(dir),
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
	svc1File := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.hello-snap.svc1.service")
	svc2File := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.hello-snap.svc2.service")

	dir := dirs.StripRootDir(filepath.Join(dirs.SnapMountDir, "hello-snap", "12.mount"))
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
WorkingDirectory=/var/snap/hello-snap/12
ExecStop=/usr/bin/snap run --command=stop hello-snap.%[1]s
ExecStopPost=/usr/bin/snap run --command=post-stop hello-snap.%[1]s
TimeoutStopSec=30
Type=forking
%[3]s
[Install]
WantedBy=multi-user.target
`
	svc1Content := fmt.Sprintf(template,
		"svc1",
		systemd.EscapeUnitNamePath(dir),
		"",
	)

	err := os.MkdirAll(filepath.Dir(svc1File), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(svc1File, []byte(svc1Content), 0644)
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
		"",
	))

	// and svc1 didn't change
	c.Assert(svc1File, testutil.FileEquals, fmt.Sprintf(template,
		"svc1",
		systemd.EscapeUnitNamePath(dir),
		"",
	))
}

func (s *servicesTestSuite) TestEnsureSnapServicesNoChangeNoop(c *C) {
	info := snaptest.MockSnap(c, packageHello, &snap.SideInfo{Revision: snap.R(12)})

	// pretend we already have a unit file setup
	svcFile := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.hello-snap.svc1.service")

	dir := dirs.StripRootDir(filepath.Join(dirs.SnapMountDir, "hello-snap", "12.mount"))
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
WorkingDirectory=/var/snap/hello-snap/12
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
		"",
	)

	err := os.MkdirAll(filepath.Dir(svcFile), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(svcFile, []byte(origContent), 0644)
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
	svcFile := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.hello-snap.svc1.service")

	dir := dirs.StripRootDir(filepath.Join(dirs.SnapMountDir, "hello-snap", "12.mount"))
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
WorkingDirectory=/var/snap/hello-snap/12
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
		"",
	)

	err := os.MkdirAll(filepath.Dir(svcFile), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(svcFile, []byte(origContent), 0644)
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
		"OOMScoreAdjust=-899\n",
	))
}

func (s *servicesTestSuite) TestEnsureSnapServicesRollsback(c *C) {
	info := snaptest.MockSnap(c, packageHello, &snap.SideInfo{Revision: snap.R(12)})

	svcFile := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.hello-snap.svc1.service")

	// pretend we already have a unit file with no VitalityRank options set
	dir := dirs.StripRootDir(filepath.Join(dirs.SnapMountDir, "hello-snap", "12.mount"))
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
WorkingDirectory=/var/snap/hello-snap/12
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
		"",
	)

	err := os.MkdirAll(filepath.Dir(svcFile), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(svcFile, []byte(origContent), 0644)
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

	svcFile := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.hello-snap.svc1.service")

	// pretend we already have a unit file with no VitalityRank options set
	dir := dirs.StripRootDir(filepath.Join(dirs.SnapMountDir, "hello-snap", "12.mount"))
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
WorkingDirectory=/var/snap/hello-snap/12
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
	svc1File := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.hello-snap.svc1.service")
	svc2File := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.hello-snap.svc2.service")

	// pretend we already have a unit file with no VitalityRank options set
	dir := dirs.StripRootDir(filepath.Join(dirs.SnapMountDir, "hello-snap", "12.mount"))
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
WorkingDirectory=/var/snap/hello-snap/12
ExecStop=/usr/bin/snap run --command=stop hello-snap.%[1]s
ExecStopPost=/usr/bin/snap run --command=post-stop hello-snap.%[1]s
TimeoutStopSec=30
Type=forking
%[3]s
[Install]
WantedBy=multi-user.target
`

	svc1Content := fmt.Sprintf(template,
		"svc1",
		systemd.EscapeUnitNamePath(dir),
		"",
	)
	svc2Content := fmt.Sprintf(template,
		"svc2",
		systemd.EscapeUnitNamePath(dir),
		"",
	)

	err := os.MkdirAll(filepath.Dir(svc1File), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(svc1File, []byte(svc1Content), 0644)
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

func (s *servicesTestSuite) TestAddSnapServicesWithInterfaceServiceSnippets(c *C) {
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
		svcFile := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.hello-snap.svc1.service")

		err := s.addSnapServices(info, false)
		c.Assert(err, IsNil, comment)
		c.Check(s.sysdLog, DeepEquals, [][]string{
			{"daemon-reload"},
		}, comment)

		dir := dirs.StripRootDir(filepath.Join(dirs.SnapMountDir, "hello-snap", "12.mount"))
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
WorkingDirectory=/var/snap/hello-snap/12
TimeoutStopSec=30
Type=simple
Delegate=true

[Install]
WantedBy=multi-user.target
`,
			systemd.EscapeUnitNamePath(dir),
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
			{"--no-reload", "disable", filepath.Base(svcFile)},
			{"daemon-reload"},
		}, comment)

		s.sysdLog = nil
	}
}

func (s *servicesTestSuite) TestAddSnapServicesWithGpioChardevInterfaceUnitSnippets(c *C) {
	const snapYaml = packageHelloNoSrv + `
 svc1:
  daemon: simple
  plugs:
   - gpio-chardev
plugs:
 gpio-chardev:
  source-chip: [chip0]
  lines: 0-3
`
	info := snaptest.MockSnap(c, snapYaml, &snap.SideInfo{Revision: snap.R(12)})
	svcFile := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.hello-snap.svc1.service")

	err := s.addSnapServices(info, false)
	c.Assert(err, IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{{"daemon-reload"}})

	dir := dirs.StripRootDir(filepath.Join(dirs.SnapMountDir, "hello-snap", "12.mount"))
	c.Assert(svcFile, testutil.FileEquals, fmt.Sprintf(`[Unit]
# Auto-generated, DO NOT EDIT
Description=Service for snap application hello-snap.svc1
Requires=%[1]s
Wants=network.target
After=%[1]s network.target snapd.apparmor.service
After=snapd.gpio-chardev-setup.target
Wants=snapd.gpio-chardev-setup.target
X-Snappy=yes

[Service]
EnvironmentFile=-/etc/environment
ExecStart=/usr/bin/snap run hello-snap.svc1
SyslogIdentifier=hello-snap.svc1
Restart=on-failure
WorkingDirectory=/var/snap/hello-snap/12
TimeoutStopSec=30
Type=simple

[Install]
WantedBy=multi-user.target
`,
		systemd.EscapeUnitNamePath(dir),
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
	c.Check(osutil.FileExists(svcFile), Equals, false)
	c.Assert(s.sysdLog, HasLen, 2)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"--no-reload", "disable", filepath.Base(svcFile)},
		{"daemon-reload"},
	})

	s.sysdLog = nil
}

func (s *servicesTestSuite) TestAddSnapServicesAndRemoveUserDaemons(c *C) {
	info := snaptest.MockSnap(c, packageHelloNoSrv+`
 svc1:
  daemon: simple
  daemon-scope: user
`, &snap.SideInfo{Revision: snap.R(12)})
	svcFile := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/user/snap.hello-snap.svc1.service")

	err := s.addSnapServices(info, false)
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
		{"--user", "--global", "--no-reload", "disable", filepath.Base(svcFile)},
		{"--user", "daemon-reload"},
	})
}

var snapdYaml = `name: snapd
version: 2.62
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

	err := s.addSnapServices(info, false)
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
			return nil, nil
		}
		return []byte("ActiveState=active\n"), errors.New("mock systemctl error")
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

	err := s.addSnapServices(info, false)
	c.Assert(err, IsNil)

	sysdLog = nil

	svcFName := "snap.wat.wat.service"

	err = wrappers.StopServices(info.Services(), nil, "", progress.Null, s.perfTimings)
	c.Assert(err, ErrorMatches, "mock systemctl error")

	c.Check(sysdLog, DeepEquals, [][]string{
		{"stop", svcFName},
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

	err := s.addSnapServices(info, false)
	c.Assert(err, IsNil)

	sysdLog = nil

	svcFName := "snap.wat.wat.service"

	err = wrappers.StopServices(info.Services(), nil, "", progress.Null, s.perfTimings)
	c.Check(err, ErrorMatches, "some user services failed to stop")
	c.Check(sysdLog, DeepEquals, [][]string{
		{"--user", "stop", svcFName},
	})
}

func (s *servicesTestSuite) TestQueryDisabledServices(c *C) {
	info := snaptest.MockSnap(c, packageHelloNoSrv+`
 svc1:
  daemon: simple
  command: bin/hello
 svc2:
  daemon: simple
  command: bin/hello
`, &snap.SideInfo{Revision: snap.R(12)})
	s.systemctlRestorer()
	// This will mock the following:
	// svc 1 will be reported as disabled
	// svc 2 will be reported as enabled
	s.systemctlRestorer = systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		s.sysdLog = append(s.sysdLog, cmd)
		return systemdtest.HandleMockAllUnitsActiveOutput(cmd, map[string]systemdtest.ServiceState{
			"snap.hello-snap.svc1.service": {
				ActiveState:   "inactive",
				UnitFileState: "disabled",
			},
			"snap.hello-snap.svc2.service": {
				ActiveState:   "inactive",
				UnitFileState: "enabled",
			},
		}), nil
	})

	disabledSvcs, err := wrappers.QueryDisabledServices(info, progress.Null)
	c.Assert(err, IsNil)

	// ensure svc1 was reported as disabled
	c.Assert(disabledSvcs, DeepEquals, &wrappers.DisabledServices{
		SystemServices: []string{"svc1"},
	})

	// the calls could be out of order in the list, since iterating over a map
	// is non-deterministic, so manually check each call
	c.Assert(s.sysdLog, HasLen, 1)
	for _, call := range s.sysdLog {
		c.Assert(call, HasLen, 4)
		c.Assert(call[0], Equals, "show")
		switch call[3] {
		case "snap.hello-snap.svc1.service", "snap.hello-snap.svc2.service":
		default:
			c.Errorf("unknown service for systemctl call: %s", call[2])
		}
	}
}

func (s *servicesTestSuite) TestQueryDisabledServicesActivatedServices(c *C) {
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
  command: bin/hello
`, &snap.SideInfo{Revision: snap.R(12)})
	s.systemctlRestorer()
	// This will mock the following:
	// svc 1 will be reported as static
	// svc 2 will be reported as enabled
	// svc 1 has two socket activations that both will be reported as disabled
	s.systemctlRestorer = systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		s.sysdLog = append(s.sysdLog, cmd)
		return systemdtest.HandleMockAllUnitsActiveOutput(cmd, map[string]systemdtest.ServiceState{
			"snap.hello-snap.svc1.service": {
				ActiveState:   "inactive",
				UnitFileState: "static",
			},
			"snap.hello-snap.svc2.service": {
				ActiveState:   "inactive",
				UnitFileState: "enabled",
			},
			"snap.hello-snap.svc1.sock1.socket": {
				ActiveState:   "inactive",
				UnitFileState: "disabled",
			},
			"snap.hello-snap.svc1.sock2.socket": {
				ActiveState:   "inactive",
				UnitFileState: "disabled",
			},
		}), nil
	})

	disabledSvcs, err := wrappers.QueryDisabledServices(info, progress.Null)
	c.Assert(err, IsNil)

	// ensure svc1 were reported as disabled
	c.Assert(disabledSvcs, DeepEquals, &wrappers.DisabledServices{
		SystemServices: []string{"svc1"},
	})

	// the calls could be out of order in the list, since iterating over a map
	// is non-deterministic, so manually check each call
	c.Assert(s.sysdLog, HasLen, 2)
	for _, call := range s.sysdLog {
		c.Assert(call, HasLen, 4)
		c.Assert(call[0], Equals, "show")
		switch call[3] {
		case "snap.hello-snap.svc1.service", "snap.hello-snap.svc2.service", "snap.hello-snap.svc3.service":
		case "snap.hello-snap.svc1.sock1.socket", "snap.hello-snap.svc1.sock2.socket":
		default:
			c.Errorf("unknown service for systemctl call: %s", call[2])
		}
	}
}

func (s *servicesTestSuite) TestQueryDisabledServicesMixedServices(c *C) {
	info := snaptest.MockSnap(c, packageHelloNoSrv+`
 svc1:
  daemon: simple
  command: bin/hello
 svc2:
  daemon: simple
  command: bin/hello
 svc3:
  daemon: simple
  command: bin/hello
  daemon-scope: user
 svc4:
  daemon: simple
  command: bin/hello
  daemon-scope: user

`, &snap.SideInfo{Revision: snap.R(12)})
	err := s.addSnapServices(info, false)
	c.Assert(err, IsNil)

	s.systemctlRestorer()
	// This will mock the following:
	// svc 1 will be reported as disabled
	// svc 2 will be reported as enabled
	s.systemctlRestorer = systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		s.sysdLog = append(s.sysdLog, cmd)
		if cmd[0] == "--user" {
			cmd = cmd[1:]
		}
		return systemdtest.HandleMockAllUnitsActiveOutput(cmd, map[string]systemdtest.ServiceState{
			"snap.hello-snap.svc1.service": {
				ActiveState:   "inactive",
				UnitFileState: "disabled",
			},
			"snap.hello-snap.svc2.service": {
				ActiveState:   "inactive",
				UnitFileState: "enabled",
			},
			"snap.hello-snap.svc3.service": {
				ActiveState:   "inactive",
				UnitFileState: "enabled",
			},
			"snap.hello-snap.svc4.service": {
				ActiveState:   "inactive",
				UnitFileState: "disabled",
			},
		}), nil
	})

	disabledSvcs, err := wrappers.QueryDisabledServices(info, progress.Null)
	c.Assert(err, IsNil)

	// ensure svc1+svc4 was reported as disabled
	uid := os.Getuid()
	c.Assert(disabledSvcs, DeepEquals, &wrappers.DisabledServices{
		SystemServices: []string{"svc1"},
		UserServices: map[int][]string{
			uid: {"svc4"},
		},
	})

	// the calls could be out of order in the list, since iterating over a map
	// is non-deterministic, so manually check each call
	c.Assert(s.sysdLog, HasLen, 5)
	for _, call := range s.sysdLog {
		if call[0] == "--user" {
			call = call[1:]
		}
		switch call[0] {
		case "show":
			switch call[2] {
			case "snap.hello-snap.svc1.service", "snap.hello-snap.svc2.service":
				// both are in one call, i.e two services
				c.Assert(call, HasLen, 4)
			case "snap.hello-snap.svc3.service", "snap.hello-snap.svc4.service":
				// both will be in separate calls, i.e one service
				c.Assert(call, HasLen, 3)
			default:
				c.Errorf("unknown service for systemctl call: %s", call[2])
			}
		case "daemon-reload":
		default:
			c.Errorf("unknown systemctl call: %s", call[1])
		}
	}
}

func (s *servicesTestSuite) TestQueryDisabledServicesUserServices(c *C) {
	info := snaptest.MockSnap(c, packageHelloNoSrv+`
 svc1:
  daemon: simple
  command: bin/hello
  daemon-scope: user
 svc2:
  daemon: simple
  command: bin/hello
  daemon-scope: user

`, &snap.SideInfo{Revision: snap.R(12)})
	err := s.addSnapServices(info, false)
	c.Assert(err, IsNil)

	s.systemctlRestorer()
	// This will mock the following:
	// svc 1 will be reported as disabled
	// svc 2 will be reported as enabled
	s.systemctlRestorer = systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		s.sysdLog = append(s.sysdLog, cmd)
		if cmd[0] != "--user" {
			return nil, fmt.Errorf("expected --user argument")
		}
		return systemdtest.HandleMockAllUnitsActiveOutput(cmd[1:], map[string]systemdtest.ServiceState{
			"snap.hello-snap.svc1.service": {
				ActiveState:   "inactive",
				UnitFileState: "disabled",
			},
			"snap.hello-snap.svc2.service": {
				ActiveState:   "inactive",
				UnitFileState: "enabled",
			},
		}), nil
	})

	disabledSvcs, err := wrappers.QueryDisabledServices(info, progress.Null)
	c.Assert(err, IsNil)

	// ensure svc1 was reported as disabled
	uid := os.Getuid()
	c.Assert(disabledSvcs, DeepEquals, &wrappers.DisabledServices{
		UserServices: map[int][]string{
			uid: {"svc1"},
		},
	})

	// the calls could be out of order in the list, since iterating over a map
	// is non-deterministic, so manually check each call
	c.Assert(s.sysdLog, HasLen, 3)
	for _, call := range s.sysdLog {
		c.Assert(call[0], Equals, "--user")
		switch call[1] {
		case "show":
			switch call[3] {
			case "snap.hello-snap.svc1.service", "snap.hello-snap.svc2.service":
				c.Assert(call, HasLen, 4)
			default:
				c.Errorf("unknown service for systemctl call: %s", call[3])
			}
		case "daemon-reload":
		default:
			c.Errorf("unknown systemctl call: %s", call[1])
		}
	}
}

func (s *servicesTestSuite) TestAddSnapServicesWithDisabledServices(c *C) {
	info := snaptest.MockSnap(c, packageHello+`
 svc2:
  command: bin/hello
  daemon: forking
`, &snap.SideInfo{Revision: snap.R(12)})

	// svc1 will be disabled
	disabledSvcs := &wrappers.DisabledServices{
		SystemServices: []string{"svc1"},
	}

	err := s.addSnapServices(info, false)
	c.Assert(err, IsNil)

	c.Assert(s.sysdLog, DeepEquals, [][]string{
		{"daemon-reload"},
	})

	s.sysdLog = nil

	opts := &wrappers.StartServicesOptions{Enable: true}
	err = wrappers.StartServices(info.Services(), disabledSvcs, opts, progress.Null, s.perfTimings)
	c.Assert(err, IsNil)

	// only svc2 should be enabled
	c.Assert(s.sysdLog, DeepEquals, [][]string{
		{"--no-reload", "enable", "snap.hello-snap.svc2.service"},
		{"daemon-reload"},
		{"start", "snap.hello-snap.svc2.service"},
	})
}

func (s *servicesTestSuite) TestAddSnapServicesWithPreseed(c *C) {
	info := snaptest.MockSnap(c, packageHello, &snap.SideInfo{Revision: snap.R(12)})

	s.systemctlRestorer()
	r := testutil.MockCommand(c, "systemctl", "exit 1")
	defer r.Restore()

	preseeding := true
	err := s.addSnapServices(info, preseeding)
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
			sysServices = append(sysServices, cmd[1:]...)
		} else if cmd[0] == "--user" && cmd[1] == "stop" {
			userServices = append(userServices, cmd[2:]...)
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

	err := s.addSnapServices(info, false)
	c.Assert(err, IsNil)

	sysServices = nil
	userServices = nil

	err = wrappers.StopServices(info.Services(), nil, "", &progress.Null, s.perfTimings)
	c.Assert(err, IsNil)

	sort.Strings(sysServices)
	c.Check(sysServices, DeepEquals, []string{
		"snap.hello-snap.svc1.service",
		"snap.hello-snap.svc1.sock1.socket", "snap.hello-snap.svc1.sock2.socket",
	})
	sort.Strings(userServices)
	c.Check(userServices, DeepEquals, []string{
		"snap.hello-snap.svc2.service",
		"snap.hello-snap.svc2.sock1.socket", "snap.hello-snap.svc2.sock2.socket",
	})
}

func (s *servicesTestSuite) TestStopStartServicesWithSocketsDisableAndEnable(c *C) {
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

	err := s.addSnapServices(info, false)
	c.Assert(err, IsNil)

	sorted := info.Services()
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Name < sorted[j].Name
	})

	// Verify the expected behaviour of StopServices when activations are in play. We expect it stop all services,
	// including activations, and then disable all the services.
	err = wrappers.StopServices(sorted, &wrappers.StopServicesOptions{Disable: true}, "", &progress.Null, s.perfTimings)
	c.Assert(err, IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"daemon-reload"},
		{"--user", "daemon-reload"},
		// Expect that we are stopping the user services
		{"--user", "stop", "snap.hello-snap.svc2.sock1.socket"},
		{"--user", "show", "--property=ActiveState", "snap.hello-snap.svc2.sock1.socket"},
		{"--user", "stop", "snap.hello-snap.svc2.sock2.socket"},
		{"--user", "show", "--property=ActiveState", "snap.hello-snap.svc2.sock2.socket"},
		{"--user", "stop", "snap.hello-snap.svc2.service"},
		{"--user", "show", "--property=ActiveState", "snap.hello-snap.svc2.service"},
		// Expect that we are disabling the user services afterwards
		{"--user", "--no-reload", "disable", "snap.hello-snap.svc2.sock1.socket", "snap.hello-snap.svc2.sock2.socket", "snap.hello-snap.svc2.service"},
		{"--user", "daemon-reload"},
		// Expect that we then stop the system services
		{"stop", "snap.hello-snap.svc1.sock1.socket"},
		{"show", "--property=ActiveState", "snap.hello-snap.svc1.sock1.socket"},
		{"stop", "snap.hello-snap.svc1.sock2.socket"},
		{"show", "--property=ActiveState", "snap.hello-snap.svc1.sock2.socket"},
		{"stop", "snap.hello-snap.svc1.service"},
		{"show", "--property=ActiveState", "snap.hello-snap.svc1.service"},
		// And then disable them :-)
		{"--no-reload", "disable", "snap.hello-snap.svc1.sock1.socket", "snap.hello-snap.svc1.sock2.socket", "snap.hello-snap.svc1.service"},
		{"daemon-reload"},
		{"--user", "--global", "--no-reload", "disable", "snap.hello-snap.svc2.sock1.socket", "snap.hello-snap.svc2.sock2.socket", "snap.hello-snap.svc2.service"},
	})

	// For activated services, we expect StartServices to only affect the activation mechanisms
	// when starting/enabling.
	s.sysdLog = nil
	err = wrappers.StartServices(sorted, nil, &wrappers.StartServicesOptions{Enable: true}, &progress.Null, s.perfTimings)
	c.Assert(err, IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"--no-reload", "enable", "snap.hello-snap.svc1.sock1.socket", "snap.hello-snap.svc1.sock2.socket"},
		{"daemon-reload"},
		{"--user", "--global", "--no-reload", "enable", "snap.hello-snap.svc2.sock1.socket", "snap.hello-snap.svc2.sock2.socket"},
		{"start", "snap.hello-snap.svc1.sock1.socket"},
		{"start", "snap.hello-snap.svc1.sock2.socket"},
		{"--user", "--no-reload", "enable", "snap.hello-snap.svc2.sock1.socket", "snap.hello-snap.svc2.sock2.socket"},
		{"--user", "daemon-reload"},
		{"--user", "start", "snap.hello-snap.svc2.sock1.socket"},
		{"--user", "start", "snap.hello-snap.svc2.sock2.socket"},
	})
}

func (s *servicesTestSuite) TestStartServicesWithServiceScope(c *C) {
	info := snaptest.MockSnap(c, packageHelloNoSrv+`
 svc1:
  daemon: simple
 svc2:
  daemon: simple
  daemon-scope: user
`, &snap.SideInfo{Revision: snap.R(12)})

	err := s.addSnapServices(info, false)
	c.Assert(err, IsNil)

	sorted := info.Services()
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Name < sorted[j].Name
	})

	err = wrappers.StartServices(sorted, nil, &wrappers.StartServicesOptions{ScopeOptions: wrappers.ScopeOptions{Scope: wrappers.ServiceScopeUser}}, &progress.Null, s.perfTimings)
	c.Assert(err, IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"daemon-reload"},
		{"--user", "daemon-reload"},
		{"--user", "start", "snap.hello-snap.svc2.service"},
	})

	// Reset the sysd log
	s.sysdLog = nil

	err = wrappers.StartServices(sorted, nil, &wrappers.StartServicesOptions{ScopeOptions: wrappers.ScopeOptions{Scope: wrappers.ServiceScopeSystem}}, &progress.Null, s.perfTimings)
	c.Assert(err, IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"start", "snap.hello-snap.svc1.service"},
	})
}

func (s *servicesTestSuite) TestStopServicesWithServiceScope(c *C) {
	info := snaptest.MockSnap(c, packageHelloNoSrv+`
 svc1:
  daemon: simple
 svc2:
  daemon: simple
  daemon-scope: user
`, &snap.SideInfo{Revision: snap.R(12)})

	err := s.addSnapServices(info, false)
	c.Assert(err, IsNil)

	sorted := info.Services()
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Name < sorted[j].Name
	})

	err = wrappers.StopServices(sorted, &wrappers.StopServicesOptions{ScopeOptions: wrappers.ScopeOptions{Scope: wrappers.ServiceScopeUser}}, "", &progress.Null, s.perfTimings)
	c.Assert(err, IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"daemon-reload"},
		{"--user", "daemon-reload"},
		{"--user", "stop", "snap.hello-snap.svc2.service"},
		{"--user", "show", "--property=ActiveState", "snap.hello-snap.svc2.service"},
	})

	// Reset the sysd log
	s.sysdLog = nil

	err = wrappers.StopServices(sorted, &wrappers.StopServicesOptions{ScopeOptions: wrappers.ScopeOptions{Scope: wrappers.ServiceScopeSystem}}, "", &progress.Null, s.perfTimings)
	c.Assert(err, IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"stop", "snap.hello-snap.svc1.service"},
		{"show", "--property=ActiveState", "snap.hello-snap.svc1.service"},
	})
}

func (s *servicesTestSuite) TestStartServicesWithDisabledActivatedService(c *C) {
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
  command: bin/hello
`, &snap.SideInfo{Revision: snap.R(12)})

	err := s.addSnapServices(info, false)
	c.Assert(err, IsNil)

	sorted := info.Services()
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Name < sorted[j].Name
	})

	disabledSvcs := &wrappers.DisabledServices{
		SystemServices: []string{"svc1"},
	}

	// When providing disabledServices (i.e during install), we want to make sure that
	// the list of disabled services is honored, including their activation units.
	s.sysdLog = nil
	err = wrappers.StartServices(sorted, disabledSvcs, &wrappers.StartServicesOptions{Enable: true}, &progress.Null, s.perfTimings)
	c.Assert(err, IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		// Expect only calls related to svc2
		{"--no-reload", "enable", "snap.hello-snap.svc2.service"},
		{"daemon-reload"},
		{"start", "snap.hello-snap.svc2.service"},
	})
}

func (s *servicesTestSuite) TestStartServicesWithDisabledUserServiceTriggersNoGlobalEnable(c *C) {
	info := snaptest.MockSnap(c, packageHelloNoSrv+`
 svc1:
  daemon: simple
  command: bin/hello
  daemon-scope: user
 svc2:
  daemon: simple
  command: bin/hello
  daemon-scope: user
`, &snap.SideInfo{Revision: snap.R(12)})

	err := s.addSnapServices(info, false)
	c.Assert(err, IsNil)

	sorted := info.Services()
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Name < sorted[j].Name
	})

	// Providing a disabled map for one of the users will indicate that
	// the service was pre-existing, meaning that it should not receive
	// a global enable (i.e this has already occurred). Otherwise did
	// would override the current enable-status of the services for all
	// users
	uid := os.Getuid()
	disabledSvcs := &wrappers.DisabledServices{
		// this will do nothing, svc1 is not a system service
		SystemServices: []string{"svc1"},
		UserServices: map[int][]string{
			uid: {"svc2"},
		},
	}

	s.sysdLog = nil
	err = wrappers.StartServices(sorted, disabledSvcs, &wrappers.StartServicesOptions{Enable: true}, &progress.Null, s.perfTimings)
	c.Assert(err, IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		// Meaning we should only see the global enable for svc1 (since that is new)
		{"--user", "--global", "--no-reload", "enable", "snap.hello-snap.svc1.service"},

		// And only see attempts at starting and enabling svc1
		{"--user", "--no-reload", "enable", "snap.hello-snap.svc1.service"},
		{"--user", "daemon-reload"},
		{"--user", "start", "snap.hello-snap.svc1.service"},
	})
}

func (s *servicesTestSuite) TestStartServicesStopsServicesIncludingActivation(c *C) {
	s.systemctlRestorer()
	s.systemctlRestorer = systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		s.sysdLog = append(s.sysdLog, cmd)
		// Inject an error into the last socket of the last service so we can see the full
		// 'undo' logic chain
		if len(cmd) == 3 && cmd[1] == "start" && cmd[2] == "snap.hello-snap.svc2.sock2.socket" {
			return []byte("no"), fmt.Errorf("mock error")
		}
		return []byte("ActiveState=inactive\n"), nil
	})

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

	err := s.addSnapServices(info, false)
	c.Assert(err, IsNil)

	sorted := info.Services()
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Name < sorted[j].Name
	})

	err = wrappers.StartServices(sorted, nil, &wrappers.StartServicesOptions{Enable: true}, &progress.Null, s.perfTimings)
	c.Check(err, NotNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		// Enable phase for the service activation units, we have one set of system daemon and one set of user daemon
		{"daemon-reload"},
		{"--user", "daemon-reload"},
		{"--no-reload", "enable", "snap.hello-snap.svc1.sock1.socket", "snap.hello-snap.svc1.sock2.socket"},
		{"daemon-reload"},
		{"--user", "--global", "--no-reload", "enable", "snap.hello-snap.svc2.sock1.socket", "snap.hello-snap.svc2.sock2.socket"},

		// Start phase for system service activation units
		{"start", "snap.hello-snap.svc1.sock1.socket"},
		{"start", "snap.hello-snap.svc1.sock2.socket"},

		// Start phase for user service activation units, we have rigged the system here to
		// fail on this step, so we can test undo logic.
		{"--user", "--no-reload", "enable", "snap.hello-snap.svc2.sock1.socket", "snap.hello-snap.svc2.sock2.socket"},
		{"--user", "daemon-reload"},
		{"--user", "start", "snap.hello-snap.svc2.sock1.socket"},
		{"--user", "start", "snap.hello-snap.svc2.sock2.socket"},

		// It failed, we attempt to stop all started user services again
		{"--user", "stop", "snap.hello-snap.svc2.sock1.socket"},
		{"--user", "show", "--property=ActiveState", "snap.hello-snap.svc2.sock1.socket"},
		{"--user", "--no-reload", "disable", "snap.hello-snap.svc2.sock1.socket", "snap.hello-snap.svc2.sock2.socket"},
		{"--user", "daemon-reload"},

		// Stop system services again
		{"stop", "snap.hello-snap.svc1.sock1.socket", "snap.hello-snap.svc1.sock2.socket", "snap.hello-snap.svc1.service"},
		{"show", "--property=ActiveState", "snap.hello-snap.svc1.sock1.socket"},
		{"show", "--property=ActiveState", "snap.hello-snap.svc1.sock2.socket"},
		{"show", "--property=ActiveState", "snap.hello-snap.svc1.service"},

		// Disable phase, where the activation units are being disabled
		{"--no-reload", "disable", "snap.hello-snap.svc1.sock1.socket", "snap.hello-snap.svc1.sock2.socket"},
		{"daemon-reload"},
		{"--user", "--global", "--no-reload", "disable", "snap.hello-snap.svc2.sock1.socket", "snap.hello-snap.svc2.sock2.socket"},
	})
}

func (s *servicesTestSuite) TestStartServices(c *C) {
	info := snaptest.MockSnap(c, packageHello, &snap.SideInfo{Revision: snap.R(12)})
	svcFile := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.hello-snap.svc1.service")

	opts := &wrappers.StartServicesOptions{Enable: true}
	err := wrappers.StartServices(info.Services(), nil, opts, &progress.Null, s.perfTimings)
	c.Assert(err, IsNil)

	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"--no-reload", "enable", filepath.Base(svcFile)},
		{"daemon-reload"},
		{"start", filepath.Base(svcFile)},
	})
}

func (s *servicesTestSuite) TestStartServicesNoEnable(c *C) {
	info := snaptest.MockSnap(c, packageHello, &snap.SideInfo{Revision: snap.R(12)})
	svcFile := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.hello-snap.svc1.service")

	opts := &wrappers.StartServicesOptions{Enable: false}
	err := wrappers.StartServices(info.Services(), nil, opts, &progress.Null, s.perfTimings)
	c.Assert(err, IsNil)

	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"start", filepath.Base(svcFile)},
	})
}

func (s *servicesTestSuite) TestStartServicesUserDaemons(c *C) {
	info := snaptest.MockSnap(c, packageHelloNoSrv+`
 svc1:
  daemon: simple
  daemon-scope: user
`, &snap.SideInfo{Revision: snap.R(12)})
	svcFile := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/user/snap.hello-snap.svc1.service")

	opts := &wrappers.StartServicesOptions{Enable: true}
	err := wrappers.StartServices(info.Services(), nil, opts, &progress.Null, s.perfTimings)
	c.Assert(err, IsNil)

	c.Assert(s.sysdLog, DeepEquals, [][]string{
		{"--user", "--global", "--no-reload", "enable", filepath.Base(svcFile)},
		{"--user", "--no-reload", "enable", filepath.Base(svcFile)},
		{"--user", "daemon-reload"},
		{"--user", "start", filepath.Base(svcFile)},
	})
}

func (s *servicesTestSuite) TestStartServicesEnabledConditional(c *C) {
	info := snaptest.MockSnap(c, packageHello, &snap.SideInfo{Revision: snap.R(12)})
	svcFile := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.hello-snap.svc1.service")

	opts := &wrappers.StartServicesOptions{}
	c.Check(wrappers.StartServices(info.Services(), nil, opts, progress.Null, s.perfTimings), IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{{"start", filepath.Base(svcFile)}})
}

func (s *servicesTestSuite) TestNoStartDisabledServices(c *C) {
	svc2Name := "snap.hello-snap.svc2.service"

	info := snaptest.MockSnap(c, packageHello+`
 svc2:
  command: bin/hello
  daemon: simple
`, &snap.SideInfo{Revision: snap.R(12)})

	disabledSvcs := &wrappers.DisabledServices{
		SystemServices: []string{"svc1"},
	}
	opts := &wrappers.StartServicesOptions{Enable: true}
	err := wrappers.StartServices(info.Services(), disabledSvcs, opts, &progress.Null, s.perfTimings)
	c.Assert(err, IsNil)
	c.Assert(s.sysdLog, DeepEquals, [][]string{
		{"--no-reload", "enable", svc2Name},
		{"daemon-reload"},
		{"start", svc2Name},
	})
}

func (s *servicesTestSuite) TestAddSnapMultiServicesFailCreateCleanup(c *C) {
	// validity check: there are no service files
	svcFiles, _ := filepath.Glob(filepath.Join(dirs.SnapServicesDir, "snap.hello-snap.*.service"))
	c.Check(svcFiles, HasLen, 0)

	info := snaptest.MockSnap(c, packageHello+`
 svc2:
  daemon: potato
`, &snap.SideInfo{Revision: snap.R(12)})

	err := s.addSnapServices(info, false)
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

	// validity check: there are no service files
	svcFiles, _ := filepath.Glob(filepath.Join(dirs.SnapServicesDir, "snap.hello-snap.*.service"))
	c.Check(svcFiles, HasLen, 0)

	r := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		c.Logf("cmd: %q", cmd)
		sysdLog = append(sysdLog, cmd)
		sdcmd := cmd[0]
		if sdcmd == "show" {
			return []byte("ActiveState=inactive"), nil
		}
		if strings.HasPrefix(sdcmd, "--") {
			c.Assert(len(sdcmd) >= 2, Equals, true)
			sdcmd = cmd[1]
		}
		switch sdcmd {
		case "enable":
			numEnables++
			c.Assert(cmd, HasLen, 4)
			if cmd[2] == svc2Name {
				svc1Name, svc2Name = svc2Name, svc1Name
			}
			return nil, fmt.Errorf("failed")
		case "disable", "daemon-reload", "stop":
			return nil, nil
		default:
			panic(fmt.Sprintf("unexpected systemctl command %q", cmd))
		}
	})
	defer r()

	info := snaptest.MockSnap(c, packageHello+`
 svc2:
  command: bin/hello
  daemon: simple
`, &snap.SideInfo{Revision: snap.R(12)})

	err := s.addSnapServices(info, false)
	c.Assert(err, IsNil)

	svcFiles, _ = filepath.Glob(filepath.Join(dirs.SnapServicesDir, "snap.hello-snap.*.service"))
	c.Check(svcFiles, HasLen, 2)

	opts := &wrappers.StartServicesOptions{Enable: true}
	err = wrappers.StartServices(info.Services(), nil, opts, progress.Null, s.perfTimings)
	c.Assert(err, ErrorMatches, "failed")

	c.Check(sysdLog, DeepEquals, [][]string{
		{"daemon-reload"}, // from AddSnapServices
		{"--no-reload", "enable", svc1Name, svc2Name},
		{"--no-reload", "disable", svc1Name, svc2Name},
		{"daemon-reload"}, // cleanup
	})
}

func (s *servicesTestSuite) TestAddSnapMultiServicesStartFailOnSystemdReloadCleanup(c *C) {
	// this test might be overdoing it (it's mostly covering the same ground as the previous one), but ... :-)
	var sysdLog [][]string

	// validity check: there are no service files
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

	err := s.addSnapServices(info, false)
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

	// validity check: there are no service files
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

	err := s.addSnapServices(info, false)
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

	// validity check: there are no service files
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

	err := s.addSnapServices(info, false)
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

	sock1File := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.hello-snap.svc1.sock1.socket")
	sock2File := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.hello-snap.svc1.sock2.socket")
	sock3File := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.hello-snap.svc1.sock3.socket")

	err := s.addSnapServices(info, false)
	c.Assert(err, IsNil)

	expected := fmt.Sprintf(
		`[Socket]
Service=snap.hello-snap.svc1.service
FileDescriptorName=sock1
ListenStream=%s
SocketMode=0666

`, filepath.Join(dirs.GlobalRootDir, "/var/snap/hello-snap/common/sock1.socket"))
	c.Check(sock1File, testutil.FileContains, expected)

	expected = fmt.Sprintf(
		`[Socket]
Service=snap.hello-snap.svc1.service
FileDescriptorName=sock2
ListenStream=%s

`, filepath.Join(dirs.GlobalRootDir, "/var/snap/hello-snap/12/sock2.socket"))
	c.Check(sock2File, testutil.FileContains, expected)

	expected = fmt.Sprintf(
		`[Socket]
Service=snap.hello-snap.svc1.service
FileDescriptorName=sock3
ListenStream=%s

`, filepath.Join(dirs.GlobalRootDir, "/run/user/0/snap.hello-snap/sock3.socket"))
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

	sock1File := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/user/snap.hello-snap.svc1.sock1.socket")
	sock2File := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/user/snap.hello-snap.svc1.sock2.socket")
	sock3File := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/user/snap.hello-snap.svc1.sock3.socket")

	err := s.addSnapServices(info, false)
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

	opts := &wrappers.StartServicesOptions{Enable: true}
	err := wrappers.StartServices(svcs, nil, opts, &progress.Null, s.perfTimings)
	c.Assert(err, ErrorMatches, "failed")
	c.Assert(sysdLog, HasLen, 10, Commentf("len: %v calls: %v", len(sysdLog), sysdLog))
	c.Check(sysdLog, DeepEquals, [][]string{
		{"--no-reload", "enable", svc1Name, svc2Name},
		{"daemon-reload"},
		{"start", svc1Name},
		{"start", svc2Name}, // one of the services fails
		{"stop", svc2Name},
		{"show", "--property=ActiveState", svc2Name},
		{"stop", svc1Name},
		{"show", "--property=ActiveState", svc1Name},
		{"--no-reload", "disable", svc1Name, svc2Name},
		{"daemon-reload"},
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
			// svc3 socket fails
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

	opts := &wrappers.StartServicesOptions{Enable: true}
	err := wrappers.StartServices(apps, nil, opts, &progress.Null, s.perfTimings)
	c.Assert(err, ErrorMatches, "failed")
	c.Logf("sysdlog: %v", sysdLog)
	c.Assert(sysdLog, HasLen, 14, Commentf("len: %v calls: %v", len(sysdLog), sysdLog))
	c.Check(sysdLog, DeepEquals, [][]string{
		{"--no-reload", "enable", svc2SocketName, svc3SocketName, svc1Name},
		{"daemon-reload"},
		{"start", svc2SocketName},
		{"start", svc3SocketName}, // start failed, what follows is the cleanup
		{"stop", svc3SocketName, svc3Name},
		{"show", "--property=ActiveState", svc3SocketName},
		{"show", "--property=ActiveState", svc3Name},
		{"stop", svc2SocketName, svc2Name},
		{"show", "--property=ActiveState", svc2SocketName},
		{"show", "--property=ActiveState", svc2Name},
		{"stop", svc1Name},
		{"show", "--property=ActiveState", svc1Name},
		{"--no-reload", "disable", svc2SocketName, svc3SocketName, svc1Name},
		{"daemon-reload"},
	}, Commentf("calls: %v", sysdLog))
}

func (s *servicesTestSuite) TestStartSnapMultiServicesFailStartNoEnableNoDisable(c *C) {
	// start services is called without the enable flag (eg. as during snap
	// start foo), in which case, the cleanup does not call disable.
	var sysdLog [][]string
	svc1Name := "snap.hello-snap.svc1.service"
	svc2Name := "snap.hello-snap.svc2.service"
	svc2SocketName := "snap.hello-snap.svc2.sock1.socket"
	svc3Name := "snap.hello-snap.svc3.service"
	svc3SocketName := "snap.hello-snap.svc3.sock1.socket"

	r := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		sysdLog = append(sysdLog, cmd)
		c.Logf("call: %v", cmd)
		if len(cmd) >= 2 && cmd[0] == "start" && cmd[1] == svc1Name {
			// svc1 fails
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

	// no enable
	opts := &wrappers.StartServicesOptions{Enable: false}
	err := wrappers.StartServices(apps, nil, opts, &progress.Null, s.perfTimings)
	c.Assert(err, ErrorMatches, "failed")
	c.Logf("sysdlog: %v", sysdLog)
	c.Assert(sysdLog, HasLen, 11, Commentf("len: %v calls: %v", len(sysdLog), sysdLog))
	c.Check(sysdLog, DeepEquals, [][]string{
		{"start", svc2SocketName},
		{"start", svc3SocketName},
		{"start", svc1Name}, // start failed, what follows is the cleanup
		{"stop", svc3SocketName, svc3Name},
		{"show", "--property=ActiveState", svc3SocketName},
		{"show", "--property=ActiveState", svc3Name},
		{"stop", svc2SocketName, svc2Name},
		{"show", "--property=ActiveState", svc2SocketName},
		{"show", "--property=ActiveState", svc2Name},
		{"stop", svc1Name},
		{"show", "--property=ActiveState", svc1Name},
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
	opts := &wrappers.StartServicesOptions{Enable: true}
	err := wrappers.StartServices(svcs, nil, opts, &progress.Null, s.perfTimings)
	c.Assert(err, ErrorMatches, "some user services failed to start")
	c.Assert(sysdLog, HasLen, 10, Commentf("len: %v calls: %v", len(sysdLog), sysdLog))
	c.Check(sysdLog, DeepEquals, [][]string{
		{"--user", "--global", "--no-reload", "enable", svc1Name, svc2Name},
		{"--user", "--no-reload", "enable", "snap.hello-snap.svc1.service", "snap.hello-snap.svc2.service"},
		{"--user", "daemon-reload"},
		{"--user", "start", svc1Name},
		{"--user", "start", svc2Name}, // one of the services fails
		// session agent attempts to stop the non-failed services
		{"--user", "stop", svc1Name},
		{"--user", "show", "--property=ActiveState", svc1Name},
		{"--user", "--no-reload", "disable", "snap.hello-snap.svc1.service", "snap.hello-snap.svc2.service"},
		{"--user", "daemon-reload"},
		// and we disable previously enabled user-services
		{"--user", "--global", "--no-reload", "disable", svc1Name, svc2Name},
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

	opts := &wrappers.StartServicesOptions{Enable: true}
	err = wrappers.StartServices(sorted, nil, opts, &progress.Null, s.perfTimings)
	c.Assert(err, IsNil)
	c.Assert(sysdLog, HasLen, 5, Commentf("len: %v calls: %v", len(sysdLog), sysdLog))
	c.Check(sysdLog, DeepEquals, [][]string{
		{"--no-reload", "enable", svc1Name, svc3Name, svc2Name},
		{"daemon-reload"},
		{"start", svc1Name},
		{"start", svc3Name},
		{"start", svc2Name},
	}, Commentf("calls: %v", sysdLog))

	// change the order
	sorted[1], sorted[0] = sorted[0], sorted[1]

	// we should observe the calls done in the same order as services
	err = wrappers.StartServices(sorted, nil, opts, &progress.Null, s.perfTimings)
	c.Assert(err, IsNil)
	c.Assert(sysdLog, HasLen, 10, Commentf("len: %v calls: %v", len(sysdLog), sysdLog))
	c.Check(sysdLog[5:], DeepEquals, [][]string{
		{"--no-reload", "enable", svc3Name, svc1Name, svc2Name},
		{"daemon-reload"},
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
		file:    filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.hello-snap.svc2.service"),
		kind:    "After",
		matches: []string{info.Apps["svc1"].ServiceName()},
	}, {
		file:    filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.hello-snap.svc3.service"),
		kind:    "After",
		matches: []string{info.Apps["svc2"].ServiceName()},
	}, {
		file:    filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.hello-snap.svc3.service"),
		kind:    "Before",
		matches: []string{info.Apps["svc4"].ServiceName()},
	}, {
		file: filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.hello-snap.svc4.service"),
		kind: "After",
		matches: []string{
			info.Apps["svc1"].ServiceName(),
			info.Apps["svc2"].ServiceName(),
			info.Apps["svc3"].ServiceName(),
		},
	}}

	err := s.addSnapServices(info, false)
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

	err := s.addSnapServices(info, false)
	c.Assert(err, IsNil)

	content, err := os.ReadFile(filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.hello-snap.svc2.service"))
	c.Assert(err, IsNil)
	c.Check(strings.Contains(string(content), "\nWatchdogSec=12\n"), Equals, true)

	noWatchdog := []string{
		filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.hello-snap.svc3.service"),
		filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.hello-snap.svc4.service"),
	}
	for _, svcPath := range noWatchdog {
		content, err := os.ReadFile(svcPath)
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
	survivorFile := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.survive-snap.survivor.service")

	err := s.addSnapServices(info, false)
	c.Assert(err, IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"daemon-reload"},
	})
	s.sysdLog = nil

	apps := []*snap.AppInfo{info.Apps["survivor"]}
	opts := &wrappers.StartServicesOptions{Enable: true}
	err = wrappers.StartServices(apps, nil, opts, progress.Null, s.perfTimings)
	c.Assert(err, IsNil)

	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"--no-reload", "enable", filepath.Base(survivorFile)},
		{"daemon-reload"},
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

	survivorFile := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.survive-snap.srv.service")
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
		{mode: "sigint", expectedSig: "INT", expectedWho: "main"},
		{mode: "sigint-all", expectedSig: "INT", expectedWho: "all"},
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
		err := s.addSnapServices(info, false)
		c.Assert(err, IsNil)

		c.Check(s.sysdLog, DeepEquals, [][]string{
			{"daemon-reload"},
		})
		s.sysdLog = nil

		var apps []*snap.AppInfo
		for _, a := range info.Apps {
			apps = append(apps, a)
		}
		opts := &wrappers.StartServicesOptions{Enable: true}
		err = wrappers.StartServices(apps, nil, opts, progress.Null, s.perfTimings)
		c.Assert(err, IsNil)
		c.Check(s.sysdLog, DeepEquals, [][]string{
			{"--no-reload", "enable", filepath.Base(survivorFile)},
			{"daemon-reload"},
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
			})
		default:
			panic("not reached")
		}
	}

}

func (s *servicesTestSuite) TestStopServiceFailsButServiceIsStopped(c *C) {
	fooUnitFile := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.basic-snap.foo.service")
	gooUnitFile := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.basic-snap.goo.service")

	// Override the systemctl handler so we can inject an error when attempting to stop
	// foo service. We then make sure to report the foo service stopped, so the service
	// code continues
	var sysdLog [][]string
	r := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		sysdLog = append(sysdLog, cmd)
		switch cmd[0] {
		case "daemon-reload":
			return nil, nil
		case "stop":
			switch cmd[1] {
			case filepath.Base(fooUnitFile):
				return nil, fmt.Errorf("failed")
			default:
				return nil, nil
			}
		case "show":
			switch cmd[2] {
			case filepath.Base(fooUnitFile):
				if cmd[1] == "--property=ActiveState" {
					return []byte("ActiveState=inactive\n"), nil
				}
				return systemdtest.HandleMockAllUnitsActiveOutput(cmd, map[string]systemdtest.ServiceState{
					filepath.Base(fooUnitFile): {ActiveState: "inactive", UnitFileState: "enabled"},
				}), nil
			case filepath.Base(gooUnitFile):
				return []byte("ActiveState=inactive\n"), nil
			}
		}
		return nil, fmt.Errorf("unexpected command %v", cmd)
	})
	defer r()

	const snapYaml = `name: basic-snap
version: 1.0
apps:
 foo:
  command: bin/hello
  daemon: simple
 goo:
  command: bin/hello
  daemon: simple
`
	info := snaptest.MockSnap(c, snapYaml, &snap.SideInfo{Revision: snap.R(1)})

	err := s.addSnapServices(info, false)
	c.Assert(err, IsNil)
	c.Check(sysdLog, DeepEquals, [][]string{
		{"daemon-reload"},
	})

	services := info.Services()
	sort.Sort(snap.AppInfoBySnapApp(services))

	sysdLog = nil
	err = wrappers.StopServices(services, nil, snap.StopReasonRemove, progress.Null, s.perfTimings)
	c.Assert(err, IsNil)
	c.Check(sysdLog, DeepEquals, [][]string{
		{"stop", filepath.Base(fooUnitFile)},
		{"show", "--property=ActiveState", filepath.Base(fooUnitFile)},
		{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", filepath.Base(fooUnitFile)},
		{"stop", filepath.Base(gooUnitFile)},
		{"show", "--property=ActiveState", filepath.Base(gooUnitFile)},
	})
}

func (s *servicesTestSuite) TestStopServiceFailsAndServiceStillRunsSoMustFail(c *C) {
	fooUnitFile := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.basic-snap.foo.service")
	gooUnitFile := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.basic-snap.goo.service")

	// Override the systemctl handler so we can inject an error when attempting to stop
	// foo service. We then make sure to report the foo service stopped, so the service
	// code continues
	var sysdLog [][]string
	r := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		sysdLog = append(sysdLog, cmd)
		switch cmd[0] {
		case "daemon-reload":
			return nil, nil
		case "stop":
			switch cmd[1] {
			case filepath.Base(fooUnitFile):
				return nil, fmt.Errorf("really failed to stop service")
			default:
				return nil, nil
			}
		case "show":
			switch cmd[2] {
			case filepath.Base(fooUnitFile):
				if cmd[1] == "--property=ActiveState" {
					return []byte("ActiveState=active\n"), nil
				}
				return systemdtest.HandleMockAllUnitsActiveOutput(cmd, map[string]systemdtest.ServiceState{
					filepath.Base(fooUnitFile): {ActiveState: "active", UnitFileState: "enabled"},
				}), nil
			case filepath.Base(gooUnitFile):
				return []byte("ActiveState=inactive\n"), nil
			}
		}
		return nil, fmt.Errorf("unexpected command %v", cmd)
	})
	defer r()

	const snapYaml = `name: basic-snap
version: 1.0
apps:
 foo:
  command: bin/hello
  daemon: simple
 goo:
  command: bin/hello
  daemon: simple
`
	info := snaptest.MockSnap(c, snapYaml, &snap.SideInfo{Revision: snap.R(1)})

	err := s.addSnapServices(info, false)
	c.Assert(err, IsNil)
	c.Check(sysdLog, DeepEquals, [][]string{
		{"daemon-reload"},
	})

	services := info.Services()
	sort.Sort(snap.AppInfoBySnapApp(services))

	sysdLog = nil
	err = wrappers.StopServices(services, nil, snap.StopReasonRemove, progress.Null, s.perfTimings)
	c.Assert(err, ErrorMatches, `really failed to stop service`)
	c.Check(sysdLog, DeepEquals, [][]string{
		{"stop", filepath.Base(fooUnitFile)},
		{"show", "--property=ActiveState", filepath.Base(fooUnitFile)},
		{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", filepath.Base(fooUnitFile)},
	})
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
	opts := &wrappers.StartServicesOptions{Enable: true}
	err := wrappers.StartServices(apps, nil, opts, &progress.Null, s.perfTimings)
	c.Assert(err, IsNil)
	c.Assert(s.sysdLog, HasLen, 8, Commentf("len: %v calls: %v", len(s.sysdLog), s.sysdLog))
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"--no-reload", "enable", svc2Sock, svc1Name},
		{"daemon-reload"},
		{"--user", "--global", "--no-reload", "enable", svc3Sock},
		{"start", svc2Sock},
		{"start", svc1Name},
		{"--user", "--no-reload", "enable", svc3Sock},
		{"--user", "daemon-reload"},
		{"--user", "start", svc3Sock},
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
	opts := &wrappers.StartServicesOptions{Enable: true}
	err := wrappers.StartServices(apps, nil, opts, &progress.Null, s.perfTimings)
	c.Assert(err, IsNil)
	c.Assert(s.sysdLog, HasLen, 8, Commentf("len: %v calls: %v", len(s.sysdLog), s.sysdLog))
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"--no-reload", "enable", svc2Timer, svc1Name},
		{"daemon-reload"},
		{"--user", "--global", "--no-reload", "enable", svc3Timer},
		{"start", svc2Timer},
		{"start", svc1Name},
		{"--user", "--no-reload", "enable", svc3Timer},
		{"--user", "daemon-reload"},
		{"--user", "start", svc3Timer},
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
	opts := &wrappers.StartServicesOptions{Enable: true}
	err := wrappers.StartServices(apps, nil, opts, &progress.Null, s.perfTimings)
	c.Assert(err, ErrorMatches, "failed")
	c.Assert(sysdLog, HasLen, 10, Commentf("len: %v calls: %v", len(sysdLog), sysdLog))
	c.Check(sysdLog, DeepEquals, [][]string{
		{"--no-reload", "enable", svc2Timer, svc1Name},
		{"daemon-reload"},
		{"start", svc2Timer}, // this call fails
		{"stop", svc2Timer, svc2Name},
		{"show", "--property=ActiveState", svc2Timer},
		{"show", "--property=ActiveState", svc2Name},
		{"stop", svc1Name},
		{"show", "--property=ActiveState", svc1Name},
		{"--no-reload", "disable", svc2Timer, svc1Name},
		{"daemon-reload"},
	}, Commentf("calls: %v", sysdLog))
}

func (s *servicesTestSuite) TestAddRemoveSnapWithTimersAddsRemovesTimerFiles(c *C) {
	info := snaptest.MockSnap(c, packageHello+`
 svc2:
  command: bin/hello
  daemon: simple
  timer: 10:00-12:00
`, &snap.SideInfo{Revision: snap.R(12)})

	err := s.addSnapServices(info, false)
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

	err := s.addSnapServices(info, false)
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
		err := s.addSnapServices(info, false)
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
	err := s.addSnapServices(info, false)
	c.Assert(err, IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"daemon-reload"},
	})
	s.sysdLog = nil

	apps := []*snap.AppInfo{info.Apps["svc1"], info.Apps["svc2"], info.Apps["svc3"]}
	opts := &wrappers.StartServicesOptions{Enable: true}
	err = wrappers.StartServices(apps, nil, opts, progress.Null, s.perfTimings)
	c.Assert(err, IsNil)

	c.Assert(s.sysdLog, HasLen, 5, Commentf("len: %v calls: %v", len(s.sysdLog), s.sysdLog))
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"--no-reload", "enable", svc1Socket, svc2Timer, svc3Name},
		{"daemon-reload"},
		{"start", svc1Socket},
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

	err := s.addSnapServices(info, false)
	c.Assert(err, IsNil)

	content, err := os.ReadFile(filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.hello-snap.svc2.service"))
	c.Assert(err, IsNil)
	c.Check(strings.Contains(string(content), "\nRestartSec=12\n"), Equals, true)

	content, err = os.ReadFile(filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.hello-snap.svc3.service"))
	c.Assert(err, IsNil)
	c.Check(strings.Contains(string(content), "RestartSec="), Equals, false)
}

func (s *servicesTestSuite) TestAddRemoveSnapServiceWithSnapd(c *C) {
	info := makeMockSnapdSnap(c)

	err := s.addSnapServices(info, false)
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

	err := s.addSnapServices(info, false)
	c.Assert(err, IsNil)

	s.sysdLog = nil
	opts := wrappers.RestartServicesOptions{Reload: true, AlsoEnabledNonActive: true}
	c.Assert(wrappers.RestartServices(info.Services(), nil, &opts, progress.Null, s.perfTimings), IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", srvFile},
		{"reload-or-restart", srvFile},
	})

	s.sysdLog = nil
	opts.Reload = false
	c.Assert(wrappers.RestartServices(info.Services(), nil, &opts, progress.Null, s.perfTimings), IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", srvFile},
		{"stop", srvFile},
		{"show", "--property=ActiveState", srvFile},
		{"start", srvFile},
	})

	s.sysdLog = nil
	c.Assert(wrappers.RestartServices(info.Services(), nil, &wrappers.RestartServicesOptions{AlsoEnabledNonActive: true}, progress.Null, s.perfTimings), IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", srvFile},
		{"stop", srvFile},
		{"show", "--property=ActiveState", srvFile},
		{"start", srvFile},
	})
}

func (s *servicesTestSuite) TestReloadOrRestartFailsRestart(c *C) {
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
		if cmd[0] == "stop" && cmd[1] == srvFile {
			return nil, fmt.Errorf("oh noes")
		}
		if out := systemdtest.HandleMockAllUnitsActiveOutput(cmd, nil); out != nil {
			return out, nil
		}
		return []byte("ActiveState=inactive\n"), nil
	})
	defer r()

	err := s.addSnapServices(info, false)
	c.Assert(err, IsNil)

	s.sysdLog = nil
	opts := wrappers.RestartServicesOptions{AlsoEnabledNonActive: true}
	c.Assert(wrappers.RestartServices(info.Services(), nil, &opts, progress.Null, s.perfTimings), ErrorMatches, `oh noes`)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", srvFile},
		{"stop", srvFile},
		{"show", "--property=ActiveState", srvFile},
	})
}

func (s *servicesTestSuite) TestReloadOrRestartButNotInRightScope(c *C) {
	const snapYaml = `name: test-snap
version: 1.0
apps:
  foo:
    command: bin/foo
    daemon: simple
`
	info := snaptest.MockSnap(c, snapYaml, &snap.SideInfo{Revision: snap.R(1)})
	srvFile := "snap.test-snap.foo.service"

	r := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		s.sysdLog = append(s.sysdLog, cmd)
		if out := systemdtest.HandleMockAllUnitsActiveOutput(cmd, nil); out != nil {
			return out, nil
		}
		return []byte("ActiveState=inactive\n"), nil
	})
	defer r()

	err := s.addSnapServices(info, false)
	c.Assert(err, IsNil)

	opts := wrappers.RestartServicesOptions{ScopeOptions: wrappers.ScopeOptions{Scope: wrappers.ServiceScopeUser}}
	c.Assert(wrappers.RestartServices(info.Services(), nil, &opts, progress.Null, s.perfTimings), IsNil)
	c.Assert(err, IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		// Only invocations from querying status
		{"daemon-reload"},
		{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", srvFile},
	})
}

func (s *servicesTestSuite) TestReloadOrRestartUserDaemons(c *C) {
	const snapYaml = `name: test-snap
version: 1.0
apps:
  foo:
    command: bin/foo
    daemon: simple
    daemon-scope: user
`
	info := snaptest.MockSnap(c, snapYaml, &snap.SideInfo{Revision: snap.R(1)})
	srvFile := "snap.test-snap.foo.service"

	r := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		s.sysdLog = append(s.sysdLog, cmd)
		if out := systemdtest.HandleMockAllUnitsActiveOutput(cmd, nil); out != nil {
			return out, nil
		}
		return []byte(`Type=notify
Id=snap.test-snap.foo.service
Names=snap.test-snap.foo.service
ActiveState=inactive
UnitFileState=enabled
NeedDaemonReload=no
`), nil
	})
	defer r()

	err := s.addSnapServices(info, false)
	c.Assert(err, IsNil)

	s.sysdLog = nil
	opts := wrappers.RestartServicesOptions{Reload: true, AlsoEnabledNonActive: true}
	c.Assert(wrappers.RestartServices(info.Services(), nil, &opts, progress.Null, s.perfTimings), IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"--user", "show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", srvFile},
		{"--user", "reload-or-restart", srvFile},
	})

	s.sysdLog = nil
	opts.Reload = false
	c.Assert(wrappers.RestartServices(info.Services(), nil, &opts, progress.Null, s.perfTimings), IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"--user", "show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", srvFile},
		{"--user", "stop", srvFile},
		{"--user", "show", "--property=ActiveState", srvFile},
		{"--user", "start", srvFile},
	})

	s.sysdLog = nil
	c.Assert(wrappers.RestartServices(info.Services(), nil, &wrappers.RestartServicesOptions{AlsoEnabledNonActive: true}, progress.Null, s.perfTimings), IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"--user", "show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", srvFile},
		{"--user", "stop", srvFile},
		{"--user", "show", "--property=ActiveState", srvFile},
		{"--user", "start", srvFile},
	})
}

func (s *servicesTestSuite) TestReloadOrRestartUserDaemonsFails(c *C) {
	const snapYaml = `name: test-snap
version: 1.0
apps:
  foo:
    command: bin/foo
    daemon: simple
    daemon-scope: user
`
	info := snaptest.MockSnap(c, snapYaml, &snap.SideInfo{Revision: snap.R(1)})
	srvFile := "snap.test-snap.foo.service"

	r := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		s.sysdLog = append(s.sysdLog, cmd)
		// fail when restarting
		if cmd[0] == "--user" && cmd[1] == "reload-or-restart" {
			return nil, fmt.Errorf("oh noes")
		}

		// otherwise return normal output
		if out := systemdtest.HandleMockAllUnitsActiveOutput(cmd, nil); out != nil {
			return out, nil
		}
		return []byte(`Type=notify
Id=snap.test-snap.foo.service
Names=snap.test-snap.foo.service
ActiveState=inactive
UnitFileState=enabled
NeedDaemonReload=no
`), nil
	})
	defer r()

	err := s.addSnapServices(info, false)
	c.Assert(err, IsNil)

	opts := wrappers.RestartServicesOptions{Reload: true, AlsoEnabledNonActive: true}
	err = wrappers.RestartServices(info.Services(), nil, &opts, progress.Null, s.perfTimings)
	c.Assert(err, ErrorMatches, `some user services failed to restart`)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"--user", "daemon-reload"},
		{"--user", "show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", srvFile},
		{"--user", "reload-or-restart", srvFile},
	})
}

func (s *servicesTestSuite) TestReloadOrRestartUserDaemonsButSystemScope(c *C) {
	const snapYaml = `name: test-snap
version: 1.0
apps:
  foo:
    command: bin/foo
    daemon: simple
    daemon-scope: user
`
	info := snaptest.MockSnap(c, snapYaml, &snap.SideInfo{Revision: snap.R(1)})
	srvFile := "snap.test-snap.foo.service"

	r := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		s.sysdLog = append(s.sysdLog, cmd)
		if out := systemdtest.HandleMockAllUnitsActiveOutput(cmd, nil); out != nil {
			return out, nil
		}
		return []byte(`Type=notify
Id=snap.test-snap.foo.service
Names=snap.test-snap.foo.service
ActiveState=inactive
UnitFileState=enabled
NeedDaemonReload=no
`), nil
	})
	defer r()

	err := s.addSnapServices(info, false)
	c.Assert(err, IsNil)

	opts := wrappers.RestartServicesOptions{ScopeOptions: wrappers.ScopeOptions{Scope: wrappers.ServiceScopeSystem}}
	c.Assert(wrappers.RestartServices(info.Services(), []string{srvFile}, &opts, progress.Null, s.perfTimings), IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		// Those comes from querying status of services
		{"--user", "daemon-reload"},
		{"--user", "show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", srvFile},
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

	err := s.addSnapServices(info, false)
	c.Assert(err, IsNil)

	s.sysdLog = nil
	services := info.Services()
	sort.Sort(snap.AppInfoBySnapApp(services))
	c.Assert(wrappers.RestartServices(services, nil, &wrappers.RestartServicesOptions{AlsoEnabledNonActive: true}, progress.Null, s.perfTimings), IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", srvFile1, srvFile2, srvFile3, srvFile4},
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

	// Verify that explicitly mentioning a service causes it to restart,
	// regardless of its state
	s.sysdLog = nil
	c.Assert(wrappers.RestartServices(services, []string{srvFile4}, &wrappers.RestartServicesOptions{AlsoEnabledNonActive: true}, progress.Null, s.perfTimings), IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", srvFile1, srvFile2, srvFile3, srvFile4},
		{"stop", srvFile1},
		{"show", "--property=ActiveState", srvFile1},
		{"start", srvFile1},
		{"stop", srvFile2},
		{"show", "--property=ActiveState", srvFile2},
		{"start", srvFile2},
		{"stop", srvFile3},
		{"show", "--property=ActiveState", srvFile3},
		{"start", srvFile3},
		{"stop", srvFile4},
		{"show", "--property=ActiveState", srvFile4},
		{"start", srvFile4},
	})

	// Check the restart only active service case
	s.sysdLog = nil
	c.Assert(wrappers.RestartServices(services, nil, &wrappers.RestartServicesOptions{AlsoEnabledNonActive: false}, progress.Null, s.perfTimings), IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", srvFile1, srvFile2, srvFile3, srvFile4},
		{"stop", srvFile1},
		{"show", "--property=ActiveState", srvFile1},
		{"start", srvFile1},
		{"stop", srvFile3},
		{"show", "--property=ActiveState", srvFile3},
		{"start", srvFile3},
	})
}

func (s *servicesTestSuite) TestRestartWithActivatedServicesInactive(c *C) {
	const manyServicesYaml = `name: test-snap
version: 1.0
apps:
  svc1:
    command: bin/foo
    daemon: simple
  svc2:
    daemon: simple
    plugs: [network-bind]
    sockets:
      sock1:
        listen-stream: $SNAP_DATA/sock1.socket
        socket-mode: 0666
      sock2:
        listen-stream: $SNAP_COMMON/sock2.socket
`
	srvFile1 := "snap.test-snap.svc1.service"
	srvFile2 := "snap.test-snap.svc2.service"
	srvFile2Sock1 := "snap.test-snap.svc2.sock1.socket"
	srvFile2Sock2 := "snap.test-snap.svc2.sock2.socket"

	info := snaptest.MockSnap(c, manyServicesYaml, &snap.SideInfo{Revision: snap.R(1)})

	r := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		s.sysdLog = append(s.sysdLog, cmd)
		states := map[string]systemdtest.ServiceState{
			srvFile1:      {ActiveState: "active", UnitFileState: "enabled"},
			srvFile2:      {ActiveState: "inactive", UnitFileState: "enabled"},
			srvFile2Sock1: {ActiveState: "inactive", UnitFileState: "enabled"},
			srvFile2Sock2: {ActiveState: "inactive", UnitFileState: "enabled"},
		}
		if out := systemdtest.HandleMockAllUnitsActiveOutput(cmd, states); out != nil {
			return out, nil
		}
		return []byte("ActiveState=inactive\n"), nil
	})
	defer r()

	err := s.addSnapServices(info, false)
	c.Assert(err, IsNil)

	// Restart without restarting things that were inactive to test this case,
	// which should result in just svc1 restarting
	s.sysdLog = nil
	services := info.Services()
	sort.Sort(snap.AppInfoBySnapApp(services))
	c.Assert(wrappers.RestartServices(services, nil, nil, progress.Null, s.perfTimings), IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", srvFile1, srvFile2},
		{"show", "--property=Id,ActiveState,UnitFileState,Names", srvFile2Sock1, srvFile2Sock2},
		{"stop", srvFile1},
		{"show", "--property=ActiveState", srvFile1},
		{"start", srvFile1},
	})

	// Restart but with restarting non-active, this means that the activated services
	// should be restarted now
	s.sysdLog = nil
	c.Assert(wrappers.RestartServices(services, nil, &wrappers.RestartServicesOptions{AlsoEnabledNonActive: true}, progress.Null, s.perfTimings), IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", srvFile1, srvFile2},
		{"show", "--property=Id,ActiveState,UnitFileState,Names", srvFile2Sock1, srvFile2Sock2},
		{"stop", srvFile1},
		{"show", "--property=ActiveState", srvFile1},
		{"start", srvFile1},
		{"stop", srvFile2Sock1, srvFile2Sock2},
		{"show", "--property=ActiveState", srvFile2Sock1},
		{"show", "--property=ActiveState", srvFile2Sock2},
		{"start", srvFile2Sock1, srvFile2Sock2},
	})

	// Restart but also reload. When reloading we expect to see different behaviour.
	// Reloading activated units is not supported by systemd, and for that reason they must
	// not appear in the list of systemctl calls.
	// The only call we expect to appear here is the primary service of svc1 as
	// that one is the only one reported as active here.
	s.sysdLog = nil
	c.Assert(wrappers.RestartServices(services, nil, &wrappers.RestartServicesOptions{Reload: true}, progress.Null, s.perfTimings), IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", srvFile1, srvFile2},
		{"show", "--property=Id,ActiveState,UnitFileState,Names", srvFile2Sock1, srvFile2Sock2},
		{"reload-or-restart", srvFile1},
	})
}

func (s *servicesTestSuite) TestRestartWithActivatedServicesActive(c *C) {
	const manyServicesYaml = `name: test-snap
version: 1.0
apps:
  svc1:
    command: bin/foo
    daemon: simple
  svc2:
    daemon: simple
    plugs: [network-bind]
    sockets:
      sock1:
        listen-stream: $SNAP_DATA/sock1.socket
        socket-mode: 0666
      sock2:
        listen-stream: $SNAP_COMMON/sock2.socket
`
	srvFile1 := "snap.test-snap.svc1.service"
	srvFile2 := "snap.test-snap.svc2.service"
	srvFile2Sock1 := "snap.test-snap.svc2.sock1.socket"
	srvFile2Sock2 := "snap.test-snap.svc2.sock2.socket"

	info := snaptest.MockSnap(c, manyServicesYaml, &snap.SideInfo{Revision: snap.R(1)})

	r := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		s.sysdLog = append(s.sysdLog, cmd)
		states := map[string]systemdtest.ServiceState{
			srvFile1:      {ActiveState: "inactive", UnitFileState: "enabled"},
			srvFile2:      {ActiveState: "inactive", UnitFileState: "enabled"},
			srvFile2Sock1: {ActiveState: "active", UnitFileState: "enabled"},
			srvFile2Sock2: {ActiveState: "active", UnitFileState: "enabled"},
		}
		if out := systemdtest.HandleMockAllUnitsActiveOutput(cmd, states); out != nil {
			return out, nil
		}
		return []byte("ActiveState=inactive\n"), nil
	})
	defer r()

	err := s.addSnapServices(info, false)
	c.Assert(err, IsNil)

	// Restart without restarting things that were inactive,
	// which will result in activators only restarting
	s.sysdLog = nil
	services := info.Services()
	sort.Sort(snap.AppInfoBySnapApp(services))
	c.Assert(wrappers.RestartServices(services, nil, nil, progress.Null, s.perfTimings), IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", srvFile1, srvFile2},
		{"show", "--property=Id,ActiveState,UnitFileState,Names", srvFile2Sock1, srvFile2Sock2},
		{"stop", srvFile2Sock1, srvFile2Sock2},
		{"show", "--property=ActiveState", srvFile2Sock1},
		{"show", "--property=ActiveState", srvFile2Sock2},
		{"start", srvFile2Sock1, srvFile2Sock2},
	})

	// Restart but with restarting non-active, which means that all services
	// **except** for any activated service will restart  (svc2 primary unit).
	// The activator service units should restart, but not the main one since
	// it was reported as inactive
	s.sysdLog = nil
	c.Assert(wrappers.RestartServices(services, nil, &wrappers.RestartServicesOptions{AlsoEnabledNonActive: true}, progress.Null, s.perfTimings), IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", srvFile1, srvFile2},
		{"show", "--property=Id,ActiveState,UnitFileState,Names", srvFile2Sock1, srvFile2Sock2},
		{"stop", srvFile1},
		{"show", "--property=ActiveState", srvFile1},
		{"start", srvFile1},
		{"stop", srvFile2Sock1, srvFile2Sock2},
		{"show", "--property=ActiveState", srvFile2Sock1},
		{"show", "--property=ActiveState", srvFile2Sock2},
		{"start", srvFile2Sock1, srvFile2Sock2},
	})

	// Restart but also reload. We do not expect any services to be restarted here as
	// both the primary units are reported inactive. (Only the activation units are
	// reported active). The reason we don't expect to see the activation units restarted
	// when reloading, is that these units do not support reloading by systemd.
	s.sysdLog = nil
	c.Assert(wrappers.RestartServices(services, nil, &wrappers.RestartServicesOptions{Reload: true}, progress.Null, s.perfTimings), IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", srvFile1, srvFile2},
		{"show", "--property=Id,ActiveState,UnitFileState,Names", srvFile2Sock1, srvFile2Sock2},
	})
}

func (s *servicesTestSuite) TestRestartWithActivatedServicesActivePrimaryUnit(c *C) {
	const manyServicesYaml = `name: test-snap
version: 1.0
apps:
  svc1:
    command: bin/foo
    daemon: simple
  svc2:
    daemon: simple
    plugs: [network-bind]
    sockets:
      sock1:
        listen-stream: $SNAP_DATA/sock1.socket
        socket-mode: 0666
      sock2:
        listen-stream: $SNAP_COMMON/sock2.socket
`
	srvFile1 := "snap.test-snap.svc1.service"
	srvFile2 := "snap.test-snap.svc2.service"
	srvFile2Sock1 := "snap.test-snap.svc2.sock1.socket"
	srvFile2Sock2 := "snap.test-snap.svc2.sock2.socket"

	info := snaptest.MockSnap(c, manyServicesYaml, &snap.SideInfo{Revision: snap.R(1)})

	r := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		s.sysdLog = append(s.sysdLog, cmd)
		states := map[string]systemdtest.ServiceState{
			srvFile1:      {ActiveState: "inactive", UnitFileState: "enabled"},
			srvFile2:      {ActiveState: "active", UnitFileState: "enabled"},
			srvFile2Sock1: {ActiveState: "active", UnitFileState: "enabled"},
			srvFile2Sock2: {ActiveState: "active", UnitFileState: "enabled"},
		}
		if out := systemdtest.HandleMockAllUnitsActiveOutput(cmd, states); out != nil {
			return out, nil
		}
		return []byte("ActiveState=inactive\n"), nil
	})
	defer r()

	err := s.addSnapServices(info, false)
	c.Assert(err, IsNil)

	s.sysdLog = nil
	services := info.Services()
	sort.Sort(snap.AppInfoBySnapApp(services))
	c.Assert(wrappers.RestartServices(services, nil, nil, progress.Null, s.perfTimings), IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", srvFile1, srvFile2},
		{"show", "--property=Id,ActiveState,UnitFileState,Names", srvFile2Sock1, srvFile2Sock2},
		{"stop", srvFile2Sock1, srvFile2Sock2, srvFile2},
		{"show", "--property=ActiveState", srvFile2Sock1},
		{"show", "--property=ActiveState", srvFile2Sock2},
		{"show", "--property=ActiveState", srvFile2},
		{"start", srvFile2Sock1, srvFile2Sock2, srvFile2},
	})
}

func (s *servicesTestSuite) TestStopAndDisableServices(c *C) {
	info := snaptest.MockSnap(c, packageHelloNoSrv+`
 svc1:
  daemon: simple
`, &snap.SideInfo{Revision: snap.R(12)})
	svcFile := "snap.hello-snap.svc1.service"

	err := s.addSnapServices(info, false)
	c.Assert(err, IsNil)

	s.sysdLog = nil
	opts := &wrappers.StopServicesOptions{Disable: true}
	err = wrappers.StopServices(info.Services(), opts, "", progress.Null, s.perfTimings)
	c.Assert(err, IsNil)
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"stop", svcFile},
		{"show", "--property=ActiveState", svcFile},
		{"--no-reload", "disable", svcFile},
		{"daemon-reload"},
	})
}

func (s *servicesTestSuite) TestNewUserServiceClientNames(c *C) {
	r := osutil.MockUserLookup(func(username string) (*user.User, error) {
		switch username {
		case "test":
			return &user.User{
				Uid:      "1000",
				Gid:      "1000",
				Username: username,
				Name:     "test-user",
				HomeDir:  "~",
			}, nil
		case "root":
			return &user.User{
				Uid:      "0",
				Gid:      "0",
				Username: username,
				Name:     "root",
				HomeDir:  "/",
			}, nil
		default:
			return nil, fmt.Errorf("unexpected username in test: %s", username)
		}
	})
	defer r()

	cli, err := wrappers.NewUserServiceClientNames([]string{"root", "test"}, &progress.Null)
	c.Assert(err, IsNil)
	c.Check(cli, NotNil)
}

func (s *servicesTestSuite) TestNewUserServiceClientNamesFails(c *C) {
	r := osutil.MockUserLookup(func(username string) (*user.User, error) {
		c.Check(username, Equals, "test")
		return nil, fmt.Errorf("oh no")
	})
	defer r()

	_, err := wrappers.NewUserServiceClientNames([]string{"test"}, &progress.Null)
	c.Assert(err, ErrorMatches, `oh no`)
}
