// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package cgroup

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/strutil"
)

const (
	// From golang.org/x/sys/unix
	cgroup2SuperMagic = 0x63677270

	// The only cgroup path we expect, for v2 this is where the unified
	// hierarchy is mounted, for v1 this is usually a tmpfs mount, under
	// which the controller-hierarchies are mounted
	cgroupMountPoint = "/sys/fs/cgroup"
)

// Filesystem root defined locally to avoid dependency on the 'dirs'
// package
var rootPath = "/"

const (
	// Separate block, because iota is fun
	Unknown = iota
	V1
	V2
)

var (
	probeVersion       = Unknown
	probeErr     error = nil
)

func init() {
	dirs.AddRootDirCallback(func(root string) {
		rootPath = root
	})
	probeVersion, probeErr = probeCgroupVersion()
	// handles error case gracefully
	pickVersionSpecificImpl()
}

func pickVersionSpecificImpl() {
	switch probeVersion {
	case V1:
		pickFreezerV1Impl()
	case V2:
		pickFreezerV2Impl()
	}
}

var fsTypeForPath = fsTypeForPathImpl

func fsTypeForPathImpl(path string) (int64, error) {
	var statfs syscall.Statfs_t
	mylog.Check(syscall.Statfs(path, &statfs))

	// Typs is int32 on 386, use explicit conversion to keep the code
	// working for both
	return int64(statfs.Type), nil
}

// ProcPidPath returns the path to the cgroup file under /proc for the given
// process id.
func ProcPidPath(pid int) string {
	return filepath.Join(rootPath, fmt.Sprintf("proc/%v/cgroup", pid))
}

func probeCgroupVersion() (version int, err error) {
	cgroupMount := filepath.Join(rootPath, cgroupMountPoint)
	typ := mylog.Check2(fsTypeForPath(cgroupMount))

	if typ == cgroup2SuperMagic {
		return V2, nil
	}
	return V1, nil
}

// IsUnified returns true when a unified cgroup hierarchy is in use
func IsUnified() bool {
	version := mylog.Check2(Version())
	return version == V2
}

// Version returns the detected cgroup version
func Version() (int, error) {
	return probeVersion, probeErr
}

// GroupMatcher attempts to match the cgroup entry
type GroupMatcher interface {
	String() string
	// Match returns true when given tuple of hierarchy-ID and controllers is a match
	Match(id, maybeControllers string) bool
}

type unified struct{}

func (u *unified) Match(id, maybeControllers string) bool {
	return id == "0" && maybeControllers == ""
}
func (u *unified) String() string { return "unified hierarchy" }

// MatchUnifiedHierarchy provides matches for unified cgroup hierarchies
func MatchUnifiedHierarchy() GroupMatcher {
	return &unified{}
}

type v1NamedHierarchy struct {
	name string
}

func (n *v1NamedHierarchy) Match(_, maybeControllers string) bool {
	if !strings.HasPrefix(maybeControllers, "name=") {
		return false
	}
	name := strings.TrimPrefix(maybeControllers, "name=")
	return name == n.name
}

func (n *v1NamedHierarchy) String() string { return fmt.Sprintf("named hierarchy %q", n.name) }

// MatchV1NamedHierarchy provides a matcher for a given named v1 hierarchy
func MatchV1NamedHierarchy(hierarchyName string) GroupMatcher {
	return &v1NamedHierarchy{name: hierarchyName}
}

type v1Controller struct {
	controller string
}

func (n *v1Controller) Match(_, maybeControllers string) bool {
	controllerList := strings.Split(maybeControllers, ",")
	return strutil.ListContains(controllerList, n.controller)
}

func (n *v1Controller) String() string { return fmt.Sprintf("controller %q", n.controller) }

// MatchV1Controller provides a matches for a given v1 controller
func MatchV1Controller(controller string) GroupMatcher {
	return &v1Controller{controller: controller}
}

// ProcGroup finds the path of a given cgroup controller for provided process
// id.
func ProcGroup(pid int, matcher GroupMatcher) (string, error) {
	if matcher == nil {
		return "", fmt.Errorf("internal error: cgroup matcher is nil")
	}

	f := mylog.Check2(os.Open(ProcPidPath(pid)))

	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		// we need to find a string like:
		//   ...
		//   <id>:<controller[,controller]>:/<path>
		//   7:freezer:/snap.hello-world
		//   ...
		// See cgroups(7) for details about the /proc/[pid]/cgroup
		// format.
		l := strings.Split(scanner.Text(), ":")
		if len(l) < 3 {
			continue
		}
		id := l[0]
		maybeControllerList := l[1]
		cgroupPath := l[2]

		if !matcher.Match(id, maybeControllerList) {
			continue
		}

		return cgroupPath, nil
	}
	if scanner.Err() != nil {
		return "", scanner.Err()
	}

	return "", fmt.Errorf("cannot find %s cgroup path for pid %v", matcher, pid)
}

// MockVersion sets the reported version of cgroup support. For use in testing only
func MockVersion(mockVersion int, mockErr error) (restore func()) {
	oldVersion, oldErr := probeVersion, probeErr
	probeVersion, probeErr = mockVersion, mockErr
	pickVersionSpecificImpl()
	return func() {
		probeVersion, probeErr = oldVersion, oldErr
	}
}

// procInfoEntry describes a single line of /proc/PID/cgroup.
//
// CgroupID is the internal kernel identifier of a mounted cgroup.
// Controllers is a list of controllers in a specific cgroup
// Path is relative to the cgroup mount point.
//
// Cgroup mount point is not provided here. It must be derived by
// cross-checking with /proc/self/mountinfo. The identifier is not
// useful for this.
//
// Cgroup v1 have non-empty Controllers and CgroupId > 0.
// Cgroup v2 have empty Controllers and CgroupId == 0
type procInfoEntry struct {
	CgroupID    int
	Controllers []string
	Path        string
}

// ProcessPathInTrackingCgroup returns the path in the hierarchy of the tracking cgroup.
//
// Tracking cgroup is whichever cgroup systemd uses for tracking processes.
// On modern systems this is the v2 cgroup. On older systems it is the
// controller-less name=systemd cgroup.
//
// This function fails on systems where systemd is not used and subsequently
// cgroups are not mounted.
func ProcessPathInTrackingCgroup(pid int) (string, error) {
	fname := ProcPidPath(pid)
	// Cgroup entries we're looking for look like this:
	// 1:name=systemd:/user.slice/user-1000.slice/user@1000.service/tmux.slice/tmux@default.service
	// 0::/user.slice/user-1000.slice/user@1000.service/tmux.slice/tmux@default.service

	// A cgroup hierarchy (both v1 and v2) can be "dangling" after being
	// mounted and unmounted.
	// It will stay present in the kernel (and therefore its paths may appear
	// in the /proc/<pid>/cgroup file) as long as there are some processes in
	// it, but it will not be present in the file-system. As such, use v2
	// if it is really mounted on the filesystem, otherwise try v1.
	var useV2 bool
	if ver := mylog.Check2(Version()); err != nil {
		return "", err
	} else if ver == V2 {
		useV2 = true
	}
	entry := mylog.Check2(scanProcCgroupFile(fname, func(e *procInfoEntry) bool {
		if useV2 {
			if e.CgroupID == 0 {
				return true
			}
		} else {
			if len(e.Controllers) == 1 && e.Controllers[0] == "name=systemd" {
				return true
			}
		}
		return false
	}))

	if entry == nil {
		return "", fmt.Errorf("cannot find tracking cgroup")
	}
	return entry.Path, nil
}

// scanProcCgroupFile scans a file for /proc/PID/cgroup entries and returns the
// first one matching the given predicate.
//
// If no entry matches the predicate nil is returned without errors.
func scanProcCgroupFile(fname string, pred func(entry *procInfoEntry) bool) (*procInfoEntry, error) {
	f := mylog.Check2(os.Open(fname))

	defer f.Close()
	return scanProcCgroup(f, pred)
}

// scanProcCgroup scans a reader for /proc/PID/cgroup entries and returns the
// first one matching the given predicate.
//
// If no entry matches the predicate nil is returned without errors.
func scanProcCgroup(reader io.Reader, pred func(entry *procInfoEntry) bool) (*procInfoEntry, error) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Text()
		entry := mylog.Check2(parseProcCgroupEntry(line))

		if pred(entry) {
			return entry, nil
		}
	}
	mylog.Check(scanner.Err())

	return nil, nil
}

// parseProcCgroupEntry parses a line in format described by cgroups(7)
// Such files represent cgroup membership of a particular process.
func parseProcCgroupEntry(line string) (*procInfoEntry, error) {
	var e procInfoEntry

	fields := strings.SplitN(line, ":", 3)
	// The format is described in cgroups(7). Field delimiter is ":" but
	// there is no escaping. The First two fields cannot have colons, including
	// cgroups with custom names. The last field can have colons but those are not
	// escaped in any way.
	if len(fields) != 3 {
		return nil, fmt.Errorf("expected three fields")
	}
	// Parse cgroup ID (decimal number).
	e.CgroupID = mylog.Check2(strconv.Atoi(fields[0]))

	// Parse the comma-separated list of controllers.
	if fields[1] != "" {
		e.Controllers = strings.Split(fields[1], ",")
	}
	// The rest is the path in the hierarchy.
	e.Path = fields[2]
	return &e, nil
}
