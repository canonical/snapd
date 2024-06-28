// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2022 Canonical Ltd
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
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/systemd"
)

const (
	uuidPattern = `[0-9a-fA-F]{8}\b-[0-9a-fA-F]{4}\b-[0-9a-fA-F]{4}\b-[0-9a-fA-F]{4}\b-[0-9a-fA-F]{12}`
)

var (

	// string that looks like a hook security tag
	roughHookTagPattern         = regexp.MustCompile(`snap\.[^.]+\.hook\.[^.]+`)
	roughHookTagPatternWithUUID = regexp.MustCompile(`(snap\.[^.]+\.hook\.[^.]+)(-` + uuidPattern + `)`)
	// string that looks like an app security tag
	roughAppTagPattern         = regexp.MustCompile(`snap\.[^.]+\.[^.]+`)
	roughAppTagPatternWithUUID = regexp.MustCompile(`(snap\.[^.]+\.[^.]+)(-` + uuidPattern + `)`)
)

// securityTagFromCgroupPath returns a security tag from cgroup path.
func securityTagFromCgroupPath(path string) naming.SecurityTag {
	leaf := filepath.Base(filepath.Clean(path))

	// If the security cgroup name doesn't start with "snap." then there is no
	// point in doing other checks.
	if !strings.HasPrefix(leaf, "snap.") {
		return nil
	}

	// We are only interested in cgroup directory names that correspond to
	// services and scopes, as they contain processes that have been invoked
	// from a snap.
	// Expected format of leaf name:
	//   snap.<pkg>.<app>.service - assigned by systemd for services
	//   snap.<pkg>.<app>-<uuid>.scope - transient scope for apps
	//   snap.<pkg>.hook.<app>-<uuid>.scope - transient scope for hooks
	if ext := filepath.Ext(leaf); ext != ".service" && ext != ".scope" {
		return nil
	}

	// The original naming convention for scope transient units was
	// snap.<pkg>.<app>.<uuid>.scope, but is now
	// snap.<pkg>.<app>-<uuid>.scope.
	//
	// Check for the new patterns first, and then fall back to the original
	// patterns if those fail. This ensures that we still match service
	// units which do not specify a UUID, and also makes sure the
	// transition between these two naming conventions is smooth.
	for _, re := range []*regexp.Regexp{roughHookTagPatternWithUUID, roughAppTagPatternWithUUID} {
		// If the string matches, we expect the whole match in the
		// first position, the tag submatch in the second position, and
		// the UUID submatch in the third position.
		if matches := re.FindStringSubmatch(leaf); len(matches) == 3 {
			tag := systemd.UnitNameToSecurityTag(matches[1])
			if parsed, err := naming.ParseSecurityTag(tag); err == nil {
				return parsed
			}
		}
	}

	for _, re := range []*regexp.Regexp{roughHookTagPattern, roughAppTagPattern} {
		if maybeTag := re.FindString(leaf); maybeTag != "" {
			tag := systemd.UnitNameToSecurityTag(maybeTag)
			if parsed, err := naming.ParseSecurityTag(tag); err == nil {
				return parsed
			}
		}
	}

	return nil
}

type InstancePathsOptions struct {
	ReturnCGroupPath bool
}

// InstancePathsOfSnap returns the list of active cgroup paths for a given snap
// If options.returnCGroupPath is TRUE, it will return the path of the CGroup itself;
// but if it is FALSE, it will return the path of the file with the PIDs of the running snap
//
// The return value is a snapshot of the cgroup paths

func InstancePathsOfSnap(snapInstanceName string, options InstancePathsOptions) ([]string, error) {
	var cgroupPathToScan string
	var pathList []string

	ver, err := Version()
	if err != nil {
		return nil, err
	}

	if ver == V2 {
		// In v2 mode scan all of /sys/fs/cgroup as there is no specialization
		// anymore (each directory represents a hierarchy with equal
		// capabilities and old split into controllers is gone).
		cgroupPathToScan = filepath.Join(rootPath, cgroupMountPoint)
	} else {
		// In v1 mode scan just /sys/fs/cgroup/systemd as that is sufficient
		// for finding snap-specific cgroup names. Systemd uses this for
		// tracking and scopes and services are represented there.
		cgroupPathToScan = filepath.Join(rootPath, cgroupMountPoint, "systemd")
	}

	// Walk the cgroup tree and look for "cgroup.procs" files. Having found one
	// we try to derive the snap security tag from it. If successful and the
	// tag matches the snap we are interested in, we harvest the snapshot of
	// PIDs that belong to the cgroup and put them into a bucket associated
	// with the security tag.
	walkFunc := func(path string, fileInfo os.FileInfo, err error) error {
		if err != nil {
			// See the documentation of path/filepath.Walk. The error we get is
			// the error that was encountered while walking. We just surface
			// that error quickly.
			return err
		}

		// ignore snaps inside containers
		for _, slice := range []string{"lxc.payload", "machine.slice", "docker"} {
			if strings.HasPrefix(path, filepath.Join(cgroupPathToScan, slice)) {
				return filepath.SkipDir
			}
		}

		if fileInfo.IsDir() {
			// We don't care about directories.
			return nil
		}
		if filepath.Base(path) != "cgroup.procs" {
			// We are looking for "cgroup.procs" files. Those contain the set
			// of processes that momentarily inhabit a cgroup.
			return nil
		}
		// Now that we are confident that the file we're looking at is
		// interesting, extract the security tag from the cgroup path and check
		// if the security tag belongs to the snap we are interested in. Since
		// not all cgroups are related to snaps it is not an error if the
		// cgroup path does not denote a snap.
		cgroupPath := filepath.Dir(path)
		parsedTag := securityTagFromCgroupPath(cgroupPath)
		if parsedTag == nil {
			return nil
		}
		if parsedTag.InstanceName() != snapInstanceName {
			return nil
		}
		if options.ReturnCGroupPath {
			pathList = append(pathList, cgroupPath)
		} else {
			pathList = append(pathList, path)
		}
		// Since we've found the file we are looking for (cgroup.procs) we no
		// longer need to scan the remaining files of this directory.
		return filepath.SkipDir
	}

	// NOTE: Walk is internally performed in lexical order so the output is
	// deterministic and we don't need to sort the returned aggregated PIDs.
	if err := filepath.Walk(cgroupPathToScan, walkFunc); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return pathList, nil
}

// PidsOfSnap returns the association of security tags to PIDs.
//
// NOTE: This function returns a reliable result only if the refresh-app-awareness
// feature was enabled before all processes related to the given snap were started.
// If the feature wasn't always enabled then only service process are correctly
// accounted for.
//
// The return value is a snapshot of the pids for a given snap, grouped by
// security tag. The result may be immediately stale as processes fork and
// exit.
//
// Importantly, if the per-snap lock is held while computing the set, then the
// following guarantee is true: if a security tag is not among the results then
// no such tag can come into existence while the lock is held.
//
// This can be used to classify the activity of a given snap into activity
// classes, based on the nature of the security tags encountered.
func PidsOfSnap(snapInstanceName string) (map[string][]int, error) {
	options := InstancePathsOptions{
		ReturnCGroupPath: false,
	}
	paths, err := InstancePathsOfSnap(snapInstanceName, options)
	if err != nil {
		return nil, err
	}

	// pidsByTag maps security tag to a list of pids.
	pidsByTag := make(map[string][]int)

	for _, path := range paths {
		pids, err := pidsInFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, nil
			}
			return nil, err
		}
		cgroupPath := filepath.Dir(path)
		parsedTag := securityTagFromCgroupPath(cgroupPath)
		tag := parsedTag.String()
		pidsByTag[tag] = append(pidsByTag[tag], pids...)
	}

	return pidsByTag, nil
}
