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

	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/strutil"
)

const (
	// From golang.org/x/sys/unix
	cgroup2SuperMagic = 0x63677270

	// The only cgroup path we expect, for v2 this is where the unified
	// hierarchy is mounted, for v1 this is usually a tmpfs mount, under
	// which the controller-hierarchies are mounted
	expectedMountPoint = "/sys/fs/cgroup"
)

var (
	// Filesystem root defined locally to avoid dependency on the 'dirs'
	// package
	rootPath = "/"
)

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
	probeVersion, probeErr = probeCgroupVersion()
}

var fsTypeForPath = fsTypeForPathImpl

func fsTypeForPathImpl(path string) (int64, error) {
	var statfs syscall.Statfs_t
	if err := syscall.Statfs(path, &statfs); err != nil {
		return 0, fmt.Errorf("cannot statfs path: %v", err)
	}
	// Typs is int32 on 386, use explicit conversion to keep the code
	// working for both
	return int64(statfs.Type), nil
}

// ProcPidPath returns the path to the cgroup file under /proc for the given
// process id.
func ProcPidPath(pid int) string {
	return filepath.Join(rootPath, fmt.Sprintf("proc/%v/cgroup", pid))
}

// ControllerPathV1 returns the path to given controller assuming cgroup v1
// hierarchy
func ControllerPathV1(controller string) string {
	return filepath.Join(rootPath, expectedMountPoint, controller)
}

func probeCgroupVersion() (version int, err error) {
	cgroupMount := filepath.Join(rootPath, expectedMountPoint)
	typ, err := fsTypeForPath(cgroupMount)
	if err != nil {
		return Unknown, fmt.Errorf("cannot determine filesystem type: %v", err)
	}
	if typ == cgroup2SuperMagic {
		return V2, nil
	}
	return V1, nil
}

// IsUnified returns true when a unified cgroup hierarchy is in use
func IsUnified() bool {
	version, _ := Version()
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

	f, err := os.Open(ProcPidPath(pid))
	if err != nil {
		return "", err
	}
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

// pidsInFile returns the list of process ID in a given file.
func pidsInFile(fname string) ([]int, error) {
	file, err := os.Open(fname)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return parsePids(bufio.NewReader(file))
}

// parsePid parses a string as a process identifier.
func parsePid(text string) (int, error) {
	pid, err := strconv.Atoi(text)
	if err != nil || (err == nil && pid <= 0) {
		return 0, fmt.Errorf("cannot parse pid %q", text)
	}
	return pid, err
}

// parsePids parses a list of pids, one per line, from a reader.
func parsePids(reader io.Reader) ([]int, error) {
	scanner := bufio.NewScanner(reader)
	var pids []int
	for scanner.Scan() {
		s := scanner.Text()
		pid, err := parsePid(s)
		if err != nil {
			return nil, err
		}
		pids = append(pids, pid)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return pids, nil
}

// MockVersion sets the reported version of cgroup support. For use in testing only
func MockVersion(mockVersion int, mockErr error) (restore func()) {
	oldVersion, oldErr := probeVersion, probeErr
	probeVersion, probeErr = mockVersion, mockErr
	return func() {
		probeVersion, probeErr = oldVersion, oldErr
	}
}

// securityTagFromCgroupPath returns a security tag from cgroup path.
func securityTagFromCgroupPath(path string) (securityTag string) {
	leaf := filepath.Base(filepath.Clean(path))
	// NOTE: The things we are returning need to minimally look like security
	// tags. They are further refined and looked at below, in PidsOfSnap.
	if matched, _ := filepath.Match("snap.*.service", leaf); matched {
		return strings.TrimSuffix(leaf, ".service")
	}
	if matched, _ := filepath.Match("snap.*.scope", leaf); matched {
		// Neither dot1 nor dot2 can be -1 because the match guarantees that at
		// least two dots exist.
		dot1 := strings.IndexRune(leaf, '.')
		dot2 := strings.IndexRune(leaf[dot1+1:], '.')
		maybeTag := strings.TrimSuffix(leaf[:dot1]+leaf[dot1+dot2+1:], ".scope")
		if naming.ValidateSecurityTag(maybeTag) == nil {
			return maybeTag
		}
	}
	return ""
}

// PidsOfSnap returns the association of security tags to PIDs.
//
// NOTE: This function returns non-empty result only if refresh-app-awareness
// is enabled.
//
// The return value is a snapshot of the pids of a given snap, grouped by
// security tag. The result may be immediately stale as processes fork and
// exit but it has the following guarantee.
//
// If the per-snap lock is held while computing the set, then the following
// guarantee is true: If a security tag is not among the result then no such
// tag can come into existence while the lock is held.
//
// This can be used to classify the activity of a given snap into activity
// classes, based on the nature of the security tags encountered.
func PidsOfSnap(snapInstanceName string) (map[string][]int, error) {
	// pidsByTag maps security tag to a list of pids.
	pidsByTag := make(map[string][]int)
	securityTagPrefix := "snap." + snapInstanceName + "."

	// Walk the cgroup tree and look for "cgroup.procs" files. Having found one
	// we try to derive the snap security tag from one. If successful and the
	// tag matches the snap we are interested in we harvest the snapshot of
	// PIDs that belong to the cgroup and bin them into a bucket associated
	// with the security tag.
	walkFunc := func(path string, fileInfo os.FileInfo, err error) error {
		if err != nil || fileInfo.IsDir() {
			return err
		}
		if filepath.Base(path) != "cgroup.procs" {
			return nil
		}
		cgroupPath := filepath.Dir(path)
		securityTag := securityTagFromCgroupPath(cgroupPath)
		if securityTag == "" {
			return nil
		}
		if !strings.HasPrefix(securityTag, securityTagPrefix) {
			return nil
		}
		pids, err := pidsInFile(path)
		if err != nil {
			return err
		}
		pidsByTag[securityTag] = append(pidsByTag[securityTag], pids...)
		return filepath.SkipDir
	}

	var cgroupPathToScan string
	ver, err := Version()
	if err != nil {
		return nil, err
	}
	if ver == V2 {
		// In v2 mode scan all of /sys/fs/cgroup
		cgroupPathToScan = filepath.Join(rootPath, expectedMountPoint)
	} else {
		// In v1 mode scan just /sys/fs/cgroup/systemd
		cgroupPathToScan = filepath.Join(rootPath, expectedMountPoint, "systemd")
	}
	// NOTE: Walk is internally performed in lexical order so the output is
	// deterministic and we don't need to sort the returned aggregated PIDs.
	if err := filepath.Walk(cgroupPathToScan, walkFunc); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	return pidsByTag, nil
}
