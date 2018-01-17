// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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

package snapstate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/cmd"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
)

// featureSet contains the flag values that can be listed in assumes entries
// that this ubuntu-core actually provides.
var featureSet = map[string]bool{
	// Support for common data directory across revisions of a snap.
	"common-data-dir": true,
	// Support for the "Environment:" feature in snap.yaml
	"snap-env": true,
}

func checkAssumes(si *snap.Info) error {
	missing := ([]string)(nil)
	for _, flag := range si.Assumes {
		if strings.HasPrefix(flag, "snapd") && checkVersion(flag[5:]) {
			continue
		}
		if !featureSet[flag] {
			missing = append(missing, flag)
		}
	}
	if len(missing) > 0 {
		hint := "try to refresh the core snap"
		if release.OnClassic {
			hint = "try to update snapd and refresh the core snap"
		}
		return fmt.Errorf("snap %q assumes unsupported features: %s (%s)", si.Name(), strings.Join(missing, ", "), hint)
	}
	return nil
}

var versionExp = regexp.MustCompile(`^([1-9][0-9]*)(?:\.([0-9]+)(?:\.([0-9]+))?)?`)

func checkVersion(version string) bool {
	req := versionExp.FindStringSubmatch(version)
	if req == nil || req[0] != version {
		return false
	}

	if cmd.Version == "unknown" {
		return true // Development tree.
	}

	cur := versionExp.FindStringSubmatch(cmd.Version)
	if cur == nil {
		return false
	}

	for i := 1; i < len(req); i++ {
		if req[i] == "" {
			return true
		}
		if cur[i] == "" {
			return false
		}
		reqN, err1 := strconv.Atoi(req[i])
		curN, err2 := strconv.Atoi(cur[i])
		if err1 != nil || err2 != nil {
			panic("internal error: version regexp is broken")
		}
		if curN != reqN {
			return curN > reqN
		}
	}

	return true
}

type SnapNeedsDevModeError struct {
	Snap string
}

func (e *SnapNeedsDevModeError) Error() string {
	return fmt.Sprintf("snap %q requires devmode or confinement override", e.Snap)
}

type SnapNeedsClassicError struct {
	Snap string
}

func (e *SnapNeedsClassicError) Error() string {
	return fmt.Sprintf("snap %q requires classic confinement", e.Snap)
}

type SnapNeedsClassicSystemError struct {
	Snap string
}

func (e *SnapNeedsClassicSystemError) Error() string {
	return fmt.Sprintf("snap %q requires classic confinement which is only available on classic systems", e.Snap)
}

// determine whether the flags (and system overrides thereof) are
// compatible with the given *snap.Info
func validateFlagsForInfo(info *snap.Info, snapst *SnapState, flags Flags) error {
	switch c := info.Confinement; c {
	case snap.StrictConfinement, "":
		// strict is always fine
		return nil
	case snap.DevModeConfinement:
		// --devmode needs to be specified every time (==> ignore snapst)
		if flags.DevModeAllowed() {
			return nil
		}
		return &SnapNeedsDevModeError{
			Snap: info.Name(),
		}
	case snap.ClassicConfinement:
		if !release.OnClassic {
			return &SnapNeedsClassicSystemError{Snap: info.Name()}
		}

		if flags.Classic {
			return nil
		}

		if snapst != nil && snapst.Flags.Classic {
			return nil
		}

		return &SnapNeedsClassicError{
			Snap: info.Name(),
		}
	default:
		return fmt.Errorf("unknown confinement %q", c)
	}
}

// do a reasonably lightweight check that a snap described by Info,
// with the given SnapState and the user-specified Flags should be
// installable on the current system.
func validateInfoAndFlags(info *snap.Info, snapst *SnapState, flags Flags) error {
	if err := validateFlagsForInfo(info, snapst, flags); err != nil {
		return err
	}

	// verify we have a valid architecture
	if !arch.IsSupportedArchitecture(info.Architectures) {
		return fmt.Errorf("snap %q supported architectures (%s) are incompatible with this system (%s)", info.Name(), strings.Join(info.Architectures, ", "), arch.UbuntuArchitecture())
	}

	// check assumes
	if err := checkAssumes(info); err != nil {
		return err
	}

	return nil
}

// normPath is a helper for validateContainer. It takes a relative path (e.g. an
// app's RestartCommand, which might be empty to mean there is no such thing),
// and cleans it.
//
// * empty paths are returned as is
// * if the path is not relative, it's initial / is dropped
// * if the path goes "outside" (ie starts with ../), the empty string is
//   returned (i.e. "ignore")
// * if there's a space in the command, ignore the rest of the string
//   (see also cmd/snap-exec/main.go's comment about strings.Split)
func normPath(path string) string {
	if path == "" {
		return ""
	}

	path = strings.TrimPrefix(filepath.Clean(path), "/")
	if strings.HasPrefix(path, "../") {
		// not something inside the snap
		return ""
	}
	if idx := strings.IndexByte(path, ' '); idx > -1 {
		return path[:idx]
	}

	return path
}

var (
	ErrBadModes     = errors.New("snap is unusable due to bad permissions; contact develper")
	ErrMissingPaths = errors.New("snap is unusable due to missing files; contact developer")
)

func validateContainer(s *snap.Info, c snap.Container) error {
	// needsrx keeps track of things that need to have at least 0555 perms
	needsrx := map[string]bool{
		".":    true,
		"meta": true,
	}
	// needsx keeps track of things that need to have at least 0111 perms
	needsx := map[string]bool{}
	// needsr keeps track of things that need to have at least 0444 perms
	needsr := map[string]bool{
		"meta/snap.yaml": true,
	}
	// needsf keeps track of things that need to be regular files (or symlinks to regular files)
	needsf := map[string]bool{}
	// noskipd tracks directories we want to descend into despite not being in needs*
	noskipd := map[string]bool{}

	for _, app := range s.Apps {
		// for non-services, paths go into the needsrx bag because users
		// need rx perms to execute it
		bag := needsrx
		paths := []string{app.Command}
		if app.IsService() {
			// services' paths just need to not be skipped by the validator
			bag = noskipd
			// additional paths to check for services:
			// XXX maybe have a method on app to keep this in sync
			paths = append(paths, app.StopCommand, app.ReloadCommand, app.PostStopCommand)
		}

		for _, path := range paths {
			path = normPath(path)
			if path == "" {
				continue
			}

			needsf[path] = true
			if app.IsService() {
				needsx[path] = true
			}
			for ; path != "."; path = filepath.Dir(path) {
				bag[path] = true
			}
		}

		// completer is special :-/
		if path := normPath(app.Completer); path != "" {
			needsr[path] = true
			for path = filepath.Dir(path); path != "."; path = filepath.Dir(path) {
				needsrx[path] = true
			}
		}
	}
	// note all needsr so far need to be regular files (or symlinks)
	for k := range needsr {
		needsf[k] = true
	}
	// thing can get jumbled up
	for path := range needsrx {
		delete(needsx, path)
		delete(needsr, path)
	}
	for path := range needsx {
		if needsr[path] {
			delete(needsx, path)
			delete(needsr, path)
			needsrx[path] = true
		}
	}
	seen := make(map[string]bool, len(needsx)+len(needsrx)+len(needsr))

	// bad modes are logged instead of being returned because the end user
	// can do nothing with the info (and the developer can read the logs)
	hasBadModes := false
	err := c.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		mode := info.Mode()
		if needsrx[path] || needsx[path] || needsr[path] {
			seen[path] = true
		}
		if !needsrx[path] && !needsx[path] && !needsr[path] && !strings.HasPrefix(path, "meta/") {
			if mode.IsDir() {
				if noskipd[path] {
					return nil
				}
				return filepath.SkipDir
			}
			return nil
		}

		if needsrx[path] || mode.IsDir() {
			if mode.Perm()&0555 != 0555 {
				logger.Noticef("in snap %q: %q should be world-readable and executable, and isn't: %s", s.Name(), path, mode)
				hasBadModes = true
			}
		} else {
			if needsf[path] {
				// this assumes that if it's a symlink it's OK. Arguably we
				// should instead follow the symlink.  We'd have to expose
				// Lstat(), and guard against loops, and ...  huge can of
				// worms, and as this validator is meant as a developer aid
				// more than anything else, not worth it IMHO (as I can't
				// imagine this happening by accident).
				if mode&(os.ModeDir|os.ModeNamedPipe|os.ModeSocket|os.ModeDevice) != 0 {
					logger.Noticef("in snap %q: %q should be a regular file (or a symlink) and isn't", s.Name(), path)
					hasBadModes = true
				}
			}
			if needsx[path] || strings.HasPrefix(path, "meta/hooks/") {
				if mode.Perm()&0111 == 0 {
					logger.Noticef("in snap %q: %q should be executable, and isn't: %s", s.Name(), path, mode)
					hasBadModes = true
				}
			} else {
				// in needsr, or under meta but not a hook
				if mode.Perm()&0444 != 0444 {
					logger.Noticef("in snap %q: %q should be world-readable, and isn't: %s", s.Name(), path, mode)
					hasBadModes = true
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(seen) != len(needsx)+len(needsrx)+len(needsr) {
		for _, needs := range []map[string]bool{needsx, needsrx, needsr} {
			for path := range needs {
				if !seen[path] {
					logger.Noticef("in snap %q: path %q does not exist", s.Name(), path)
				}
			}
		}
		return ErrMissingPaths
	}

	if hasBadModes {
		return ErrBadModes
	}
	return nil
}

var openSnapFile = backend.OpenSnapFile

// checkSnap ensures that the snap can be installed.
func checkSnap(st *state.State, snapFilePath string, si *snap.SideInfo, curInfo *snap.Info, flags Flags) error {
	// This assumes that the snap was already verified or --dangerous was used.

	s, c, err := openSnapFile(snapFilePath, si)
	if err != nil {
		return err
	}

	if err := validateInfoAndFlags(s, nil, flags); err != nil {
		return err
	}

	if err := validateContainer(s, c); err != nil {
		return err
	}

	st.Lock()
	defer st.Unlock()

	for _, check := range checkSnapCallbacks {
		err := check(st, s, curInfo, flags)
		if err != nil {
			return err
		}
	}

	return nil
}

// CheckSnapCallback defines callbacks for checking a snap for installation or refresh.
type CheckSnapCallback func(st *state.State, snap, curSnap *snap.Info, flags Flags) error

var checkSnapCallbacks []CheckSnapCallback

// AddCheckSnapCallback installs a callback to check a snap for installation or refresh.
func AddCheckSnapCallback(check CheckSnapCallback) {
	checkSnapCallbacks = append(checkSnapCallbacks, check)
}

func MockCheckSnapCallbacks(checks []CheckSnapCallback) (restore func()) {
	prev := checkSnapCallbacks
	checkSnapCallbacks = checks
	return func() {
		checkSnapCallbacks = prev
	}
}

func checkCoreName(st *state.State, snapInfo, curInfo *snap.Info, flags Flags) error {
	if snapInfo.Type != snap.TypeOS {
		// not a relevant check
		return nil
	}
	if curInfo != nil {
		// already one of these installed
		return nil
	}
	core, err := CoreInfo(st)
	if err == state.ErrNoState {
		return nil
	}
	if err != nil {
		return err
	}

	// Allow installing "core" even if "ubuntu-core" is already
	// installed. Ideally we should only allow this if we know
	// this install is part of the ubuntu-core->core transition
	// (e.g. via a flag) because if this happens outside of this
	// transition we will end up with not connected interface
	// connections in the "core" snap. But the transition will
	// kick in automatically quickly so an extra flag is overkill.
	if snapInfo.Name() == "core" && core.Name() == "ubuntu-core" {
		return nil
	}

	// but generally do not allow to have two cores installed
	if core.Name() != snapInfo.Name() {
		return fmt.Errorf("cannot install core snap %q when core snap %q is already present", snapInfo.Name(), core.Name())
	}

	return nil
}

func checkGadgetOrKernel(st *state.State, snapInfo, curInfo *snap.Info, flags Flags) error {
	kind := ""
	var currentInfo func(*state.State) (*snap.Info, error)
	switch snapInfo.Type {
	case snap.TypeGadget:
		kind = "gadget"
		currentInfo = GadgetInfo
	case snap.TypeKernel:
		kind = "kernel"
		currentInfo = KernelInfo
	default:
		// not a relevant check
		return nil
	}

	currentSnap, err := currentInfo(st)
	// in firstboot we have no gadget/kernel yet - that is ok
	// first install rules are in devicestate!
	if err == state.ErrNoState {
		return nil
	}
	if err != nil {
		return fmt.Errorf("cannot find original %s snap: %v", kind, err)
	}

	if currentSnap.SnapID != "" && snapInfo.SnapID != "" {
		if currentSnap.SnapID == snapInfo.SnapID {
			// same snap
			return nil
		}
		return fmt.Errorf("cannot replace %s snap with a different one", kind)
	}

	if currentSnap.SnapID != "" && snapInfo.SnapID == "" {
		return fmt.Errorf("cannot replace signed %s snap with an unasserted one", kind)
	}

	if currentSnap.Name() != snapInfo.Name() {
		return fmt.Errorf("cannot replace %s snap with a different one", kind)
	}

	return nil
}

func checkBases(st *state.State, snapInfo, curInfo *snap.Info, flags Flags) error {
	// check if this is relevant
	if snapInfo.Type != snap.TypeApp && snapInfo.Type != snap.TypeGadget {
		return nil
	}
	if snapInfo.Base == "" {
		return nil
	}

	snapStates, err := All(st)
	if err != nil {
		return err
	}
	for otherSnap, snapst := range snapStates {
		typ, err := snapst.Type()
		if err != nil {
			return err
		}
		if typ == snap.TypeBase && otherSnap == snapInfo.Base {
			return nil
		}
	}

	return fmt.Errorf("cannot find required base %q", snapInfo.Base)
}

func init() {
	AddCheckSnapCallback(checkCoreName)
	AddCheckSnapCallback(checkGadgetOrKernel)
	AddCheckSnapCallback(checkBases)
}
