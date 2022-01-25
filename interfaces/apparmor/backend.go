// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2020 Canonical Ltd
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

// Package apparmor implements integration between snappy and
// ubuntu-core-launcher around apparmor.
//
// Snappy creates apparmor profiles for each application (for each snap)
// present in the system.  Upon each execution of ubuntu-core-launcher
// application process is launched under the profile. Prior to that the profile
// must be parsed, compiled and loaded into the kernel using the support tool
// "apparmor_parser".
//
// Each apparmor profile contains a simple <header><content><footer> structure.
// The header specifies the profile name that the launcher will use to launch a
// process under this profile.  Snappy uses "abstract identifiers" as profile
// names.
//
// The actual profiles are stored in /var/lib/snappy/apparmor/profiles.
//
// NOTE: A systemd job (apparmor.service) loads all snappy-specific apparmor
// profiles into the kernel during the boot process.
package apparmor

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
	apparmor_sandbox "github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/timings"
)

var (
	procSelfExe           = "/proc/self/exe"
	isHomeUsingNFS        = osutil.IsHomeUsingNFS
	isRootWritableOverlay = osutil.IsRootWritableOverlay
	kernelFeatures        = apparmor_sandbox.KernelFeatures
	parserFeatures        = apparmor_sandbox.ParserFeatures

	// make sure that apparmor profile fulfills the late discarding backend
	// interface
	_ interfaces.SecurityBackendDiscardingLate = (*Backend)(nil)
)

// Backend is responsible for maintaining apparmor profiles for snaps and parts of snapd.
type Backend struct {
	preseed bool
}

// Name returns the name of the backend.
func (b *Backend) Name() interfaces.SecuritySystem {
	return interfaces.SecurityAppArmor
}

// Initialize prepares customized apparmor policy for snap-confine.
func (b *Backend) Initialize(opts *interfaces.SecurityBackendOptions) error {
	if opts != nil && opts.Preseed {
		b.preseed = true
	}
	// NOTE: It would be nice if we could also generate the profile for
	// snap-confine executing from the core snap, right here, and not have to
	// do this in the Setup function below. I sadly don't think this is
	// possible because snapd must be able to install a new core and only at
	// that moment generate it.

	// Inspect the system and sets up local apparmor policy for snap-confine.
	// Local policy is included by the system-wide policy. If the local policy
	// has changed then the apparmor profile for snap-confine is reloaded.

	// Create the local policy directory if it is not there.
	if err := os.MkdirAll(dirs.SnapConfineAppArmorDir, 0755); err != nil {
		return fmt.Errorf("cannot create snap-confine policy directory: %s", err)
	}

	// Check the /proc/self/exe symlink, this is needed below but we want to
	// fail early if this fails for whatever reason.
	exe, err := os.Readlink(procSelfExe)
	if err != nil {
		return fmt.Errorf("cannot read %s: %s", procSelfExe, err)
	}

	// Location of the generated policy.
	glob := "*"
	policy := make(map[string]osutil.FileState)

	// Check if NFS is mounted at or under $HOME. Because NFS is not
	// transparent to apparmor we must alter our profile to counter that and
	// allow snap-confine to work.
	if nfs, err := isHomeUsingNFS(); err != nil {
		logger.Noticef("cannot determine if NFS is in use: %v", err)
	} else if nfs {
		policy["nfs-support"] = &osutil.MemoryFileState{
			Content: []byte(nfsSnippet),
			Mode:    0644,
		}
		logger.Noticef("snapd enabled NFS support, additional implicit network permissions granted")
	}

	// Check if '/' is on overlayfs. If so, add the necessary rules for
	// upperdir and allow snap-confine to work.
	if overlayRoot, err := isRootWritableOverlay(); err != nil {
		logger.Noticef("cannot determine if root filesystem on overlay: %v", err)
	} else if overlayRoot != "" {
		snippet := strings.Replace(overlayRootSnippet, "###UPPERDIR###", overlayRoot, -1)
		policy["overlay-root"] = &osutil.MemoryFileState{
			Content: []byte(snippet),
			Mode:    0644,
		}
		logger.Noticef("snapd enabled root filesystem on overlay support, additional upperdir permissions granted")
	}

	// Check whether apparmor_parser supports bpf capability. Some older
	// versions do not, hence the capability cannot be part of the default
	// profile of snap-confine as loading it would fail.
	if features, err := apparmor_sandbox.ParserFeatures(); err != nil {
		logger.Noticef("cannot determine apparmor_parser features: %v", err)
	} else if strutil.ListContains(features, "cap-bpf") {
		policy["cap-bpf"] = &osutil.MemoryFileState{
			Content: []byte(capabilityBPFSnippet),
			Mode:    0644,
		}
	}

	// Ensure that generated policy is what we computed above.
	created, removed, err := osutil.EnsureDirState(dirs.SnapConfineAppArmorDir, glob, policy)
	if err != nil {
		return fmt.Errorf("cannot synchronize snap-confine policy: %s", err)
	}
	if len(created) == 0 && len(removed) == 0 {
		// If the generated policy didn't change, we're all done.
		return nil
	}

	// If snapd is executing from the core snap the it means it has
	// re-executed. In that case we are no longer using the copy of
	// snap-confine from the host distribution but our own copy. We don't have
	// to re-compile and load the updated profile as that is performed by
	// setupSnapConfineReexec below.
	if strings.HasPrefix(exe, dirs.SnapMountDir) {
		return nil
	}

	// Reload the apparmor profile of snap-confine. This points to the main
	// file in /etc/apparmor.d/ as that file contains include statements that
	// load any of the files placed in /var/lib/snapd/apparmor/snap-confine/.
	// For historical reasons we may have a filename that ends with .real or
	// not.  If we do then we prefer the file ending with the name .real as
	// that is the more recent name we use.
	var profilePath string
	// TODO: fix for distros using /usr/libexec/snapd
	for _, profileFname := range []string{"usr.lib.snapd.snap-confine.real", "usr.lib.snapd.snap-confine"} {
		maybeProfilePath := filepath.Join(apparmor_sandbox.ConfDir, profileFname)
		if _, err := os.Stat(maybeProfilePath); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		profilePath = maybeProfilePath
		break
	}
	if profilePath == "" {
		// XXX: is profile mandatory on some distros?

		// There is no AppArmor profile for snap-confine, quite likely
		// AppArmor support is enabled in the kernel and relevant
		// userspace tools exist, but snap-confine was built without it,
		// nothing we need to update then.
		logger.Noticef("snap-confine apparmor profile is absent, nothing to update")
		return nil
	}

	aaFlags := skipReadCache
	if b.preseed {
		aaFlags |= skipKernelLoad
	}

	// We are not using apparmor.LoadProfiles() because it uses other cache.
	if err := loadProfiles([]string{profilePath}, apparmor_sandbox.SystemCacheDir, aaFlags); err != nil {
		// When we cannot reload the profile then let's remove the generated
		// policy. Maybe we have caused the problem so it's better to let other
		// things work.
		osutil.EnsureDirState(dirs.SnapConfineAppArmorDir, glob, nil)
		return fmt.Errorf("cannot reload snap-confine apparmor profile: %v", err)
	}
	return nil
}

// snapConfineFromSnapProfile returns the apparmor profile for
// snap-confine in the given core/snapd snap.
func snapConfineFromSnapProfile(info *snap.Info) (dir, glob string, content map[string]osutil.FileState, err error) {
	// TODO: fix this for distros using /usr/libexec/snapd when those start
	// to use reexec

	// Find the vanilla apparmor profile for snap-confine as present in the given core snap.

	// We must test the ".real" suffix first, this is a workaround for
	// https://bugs.debian.org/cgi-bin/bugreport.cgi?bug=858004
	vanillaProfilePath := filepath.Join(info.MountDir(), "/etc/apparmor.d/usr.lib.snapd.snap-confine.real")
	vanillaProfileText, err := ioutil.ReadFile(vanillaProfilePath)
	if os.IsNotExist(err) {
		vanillaProfilePath = filepath.Join(info.MountDir(), "/etc/apparmor.d/usr.lib.snapd.snap-confine")
		vanillaProfileText, err = ioutil.ReadFile(vanillaProfilePath)
	}
	if err != nil {
		return "", "", nil, fmt.Errorf("cannot open apparmor profile for vanilla snap-confine: %s", err)
	}

	// Replace the path to vanilla snap-confine with the path to the mounted snap-confine from core.
	snapConfineInCore := filepath.Join(info.MountDir(), "usr/lib/snapd/snap-confine")
	patchedProfileText := bytes.Replace(
		vanillaProfileText, []byte("/usr/lib/snapd/snap-confine"), []byte(snapConfineInCore), -1)

	// We need to add a uniqe prefix that can never collide with a
	// snap on the system. Using "snap-confine.*" is similar to
	// "snap-update-ns.*" that is already used there
	//
	// So
	//   /snap/core/111/usr/lib/snapd/snap-confine
	// becomes
	//   snap-confine.core.111
	patchedProfileName := snapConfineProfileName(info.InstanceName(), info.Revision)
	// remove other generated profiles, which is only relevant for the
	// 'core' snap on classic system where we reexec, on core systems the
	// profile is already a part of the rootfs snap
	patchedProfileGlob := fmt.Sprintf("snap-confine.%s.*", info.InstanceName())

	if info.Type() == snap.TypeSnapd {
		// with the snapd snap, things are a little different, the
		// profile is discarded only late for the revisions that are
		// being removed, also on core devices the rootfs snap and the
		// snapd snap are updated separately, so the profile needs to be
		// around for as long as the given revision of the snapd snap is
		// active, so we use the exact match such that we only replace
		// our own profile, which can happen if system was rebooted
		// before task calling the backend was finished
		patchedProfileGlob = patchedProfileName
	}

	// Return information for EnsureDirState that describes the re-exec profile for snap-confine.
	content = map[string]osutil.FileState{
		patchedProfileName: &osutil.MemoryFileState{
			Content: []byte(patchedProfileText),
			Mode:    0644,
		},
	}

	return dirs.SnapAppArmorDir, patchedProfileGlob, content, nil
}

func snapConfineProfileName(snapName string, rev snap.Revision) string {
	return fmt.Sprintf("snap-confine.%s.%s", snapName, rev)
}

// setupSnapConfineReexec will setup apparmor profiles inside the host's
// /var/lib/snapd/apparmor/profiles directory. This is needed for
// running snap-confine from the core or snapd snap.
//
// Additionally it will cleanup stale apparmor profiles it created.
func (b *Backend) setupSnapConfineReexec(info *snap.Info) error {
	if err := os.MkdirAll(dirs.SnapConfineAppArmorDir, 0755); err != nil {
		return fmt.Errorf("cannot create snap-confine policy directory: %s", err)
	}
	dir, glob, content, err := snapConfineFromSnapProfile(info)
	cache := apparmor_sandbox.CacheDir
	if err != nil {
		return fmt.Errorf("cannot compute snap-confine profile: %s", err)
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("cannot create snap-confine directory %q: %s", dir, err)
	}

	changed, removed, errEnsure := osutil.EnsureDirState(dir, glob, content)
	if len(changed) == 0 {
		// XXX: because NFS workaround is handled separately the same correct
		// snap-confine profile may need to be re-loaded. This is because the
		// profile contains include directives and those load a second file
		// that has changed outside of the scope of EnsureDirState.
		//
		// To counter that, always reload the profile by pretending it had
		// changed.
		for fname := range content {
			changed = append(changed, fname)
		}
	}
	pathnames := make([]string, len(changed))
	for i, profile := range changed {
		pathnames[i] = filepath.Join(dir, profile)
	}

	var aaFlags aaParserFlags
	if b.preseed {
		aaFlags = skipKernelLoad
	}
	errReload := loadProfiles(pathnames, cache, aaFlags)
	errUnload := unloadProfiles(removed, cache)
	if errEnsure != nil {
		return fmt.Errorf("cannot synchronize snap-confine apparmor profile: %s", errEnsure)
	}
	if errReload != nil {
		return fmt.Errorf("cannot reload snap-confine apparmor profile: %s", errReload)
	}
	if errUnload != nil {
		return fmt.Errorf("cannot unload snap-confine apparmor profile: %s", errReload)
	}
	return nil
}

// nsProfile returns name of the apparmor profile for snap-update-ns for a given snap.
func nsProfile(snapName string) string {
	return fmt.Sprintf("snap-update-ns.%s", snapName)
}

// profileGlobs returns a list of globs that describe the apparmor profiles of
// a given snap.
//
// Currently the list is just a pair. The first glob describes profiles for all
// apps and hooks while the second profile describes the snap-update-ns profile
// for the whole snap.
func profileGlobs(snapName string) []string {
	return []string{interfaces.SecurityTagGlob(snapName), nsProfile(snapName)}
}

// Determine if a profile filename is removable during core refresh/rollback.
// This is needed because core devices are also special, the apparmor cache
// gets confused too easy, especially at rollbacks, so we delete the cache. See
// Setup(), below. Some systems employ a unified cache directory where all
// apparmor cache files are stored under one location so ensure we don't remove
// the snap profiles since snapd manages them elsewhere and instead only remove
// snap-confine and system profiles (eg, as shipped by distro package manager
// or created by the administrator). snap-confine profiles are like the
// following:
// - usr.lib.snapd.snap-confine.real
// - usr.lib.snapd.snap-confine (historic)
// - snap.core.NNNN.usr.lib.snapd.snap-confine (historic)
// - var.lib.snapd.snap.core.NNNN.usr.lib.snapd.snap-confine (historic)
// - snap-confine.core.NNNN
// - snap-confine.snapd.NNNN
func profileIsRemovableOnCoreSetup(fn string) bool {
	bn := path.Base(fn)
	if strings.HasPrefix(bn, ".") {
		return false
	} else if strings.HasPrefix(bn, "snap") && !strings.HasPrefix(bn, "snap-confine.core.") && !strings.HasPrefix(bn, "snap-confine.snapd.") && !strings.Contains(bn, "usr.lib.snapd.snap-confine") {
		return false
	}
	return true
}

type profilePathsResults struct {
	changed   []string
	unchanged []string
	removed   []string
}

func (b *Backend) prepareProfiles(snapInfo *snap.Info, opts interfaces.ConfinementOptions, repo *interfaces.Repository) (prof *profilePathsResults, err error) {
	snapName := snapInfo.InstanceName()
	spec, err := repo.SnapSpecification(b.Name(), snapName)
	if err != nil {
		return nil, fmt.Errorf("cannot obtain apparmor specification for snap %q: %s", snapName, err)
	}

	// Add snippets for parallel snap installation mapping
	spec.(*Specification).AddOvername(snapInfo)

	// Add snippets derived from the layout definition.
	spec.(*Specification).AddLayout(snapInfo)

	// core on classic is special
	if snapName == "core" && release.OnClassic && apparmor_sandbox.ProbedLevel() != apparmor_sandbox.Unsupported {
		if err := b.setupSnapConfineReexec(snapInfo); err != nil {
			return nil, fmt.Errorf("cannot create host snap-confine apparmor configuration: %s", err)
		}
	}

	// Deal with the "snapd" snap - we do the setup slightly differently
	// here because this will run both on classic and on Ubuntu Core 18
	// systems but /etc/apparmor.d is not writable on core18 systems
	if snapInfo.Type() == snap.TypeSnapd && apparmor_sandbox.ProbedLevel() != apparmor_sandbox.Unsupported {
		if err := b.setupSnapConfineReexec(snapInfo); err != nil {
			return nil, fmt.Errorf("cannot create host snap-confine apparmor configuration: %s", err)
		}
	}

	// core on core devices is also special, the apparmor cache gets
	// confused too easy, especially at rollbacks, so we delete the cache.
	// See LP:#1460152 and
	// https://forum.snapcraft.io/t/core-snap-revert-issues-on-core-devices/
	//
	if (snapInfo.Type() == snap.TypeOS || snapInfo.Type() == snap.TypeSnapd) && !release.OnClassic {
		if li, err := filepath.Glob(filepath.Join(apparmor_sandbox.SystemCacheDir, "*")); err == nil {
			for _, p := range li {
				if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() && profileIsRemovableOnCoreSetup(p) {
					if err := os.Remove(p); err != nil {
						logger.Noticef("cannot remove %q: %s", p, err)
					}
				}
			}
		}
	}

	// Get the files that this snap should have
	content := b.deriveContent(spec.(*Specification), snapInfo, opts)

	dir := dirs.SnapAppArmorDir
	globs := profileGlobs(snapInfo.InstanceName())
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("cannot create directory for apparmor profiles %q: %s", dir, err)
	}
	changed, removedPaths, errEnsure := osutil.EnsureDirStateGlobs(dir, globs, content)
	// XXX: in the old code this error was reported late, after doing load/unload.
	if errEnsure != nil {
		return nil, fmt.Errorf("cannot synchronize security files for snap %q: %s", snapName, errEnsure)
	}

	// Find the set of unchanged profiles.
	unchanged := make([]string, 0, len(content)-len(changed))
	for name := range content {
		// changed is pre-sorted by EnsureDirStateGlobs
		x := sort.SearchStrings(changed, name)
		if x < len(changed) && changed[x] == name {
			continue
		}
		unchanged = append(unchanged, name)
	}
	sort.Strings(unchanged)

	changedPaths := make([]string, len(changed))
	for i, profile := range changed {
		changedPaths[i] = filepath.Join(dir, profile)
	}

	unchangedPaths := make([]string, len(unchanged))
	for i, profile := range unchanged {
		unchangedPaths[i] = filepath.Join(dir, profile)
	}

	return &profilePathsResults{changed: changedPaths, removed: removedPaths, unchanged: unchangedPaths}, nil
}

// Setup creates and loads apparmor profiles specific to a given snap.
// The snap can be in developer mode to make security violations non-fatal to
// the offending application process.
//
// This method should be called after changing plug, slots, connections between
// them or application present in the snap.
func (b *Backend) Setup(snapInfo *snap.Info, opts interfaces.ConfinementOptions, repo *interfaces.Repository, tm timings.Measurer) error {
	prof, err := b.prepareProfiles(snapInfo, opts, repo)
	if err != nil {
		return err
	}

	// Load all changed profiles with a flag that asks apparmor to skip reading
	// the cache (since we know those changed for sure).  This allows us to
	// work despite time being wrong (e.g. in the past). For more details see
	// https://forum.snapcraft.io/t/apparmor-profile-caching/1268/18
	var errReloadChanged error
	aaFlags := skipReadCache
	if b.preseed {
		aaFlags |= skipKernelLoad
	}
	timings.Run(tm, "load-profiles[changed]", fmt.Sprintf("load changed security profiles of snap %q", snapInfo.InstanceName()), func(nesttm timings.Measurer) {
		errReloadChanged = loadProfiles(prof.changed, apparmor_sandbox.CacheDir, aaFlags)
	})

	// Load all unchanged profiles anyway. This ensures those are correct in
	// the kernel even if the files on disk were not changed. We rely on
	// apparmor cache to make this performant.
	var errReloadOther error
	aaFlags = 0
	if b.preseed {
		aaFlags |= skipKernelLoad
	}
	timings.Run(tm, "load-profiles[unchanged]", fmt.Sprintf("load unchanged security profiles of snap %q", snapInfo.InstanceName()), func(nesttm timings.Measurer) {
		errReloadOther = loadProfiles(prof.unchanged, apparmor_sandbox.CacheDir, aaFlags)
	})
	errUnload := unloadProfiles(prof.removed, apparmor_sandbox.CacheDir)
	if errReloadChanged != nil {
		return errReloadChanged
	}
	if errReloadOther != nil {
		return errReloadOther
	}
	return errUnload
}

// SetupMany creates and loads apparmor profiles for multiple snaps.
// The snaps can be in developer mode to make security violations non-fatal to
// the offending application process.
// SetupMany tries to recreate all profiles without interrupting on errors, but
// collects and returns them all.
//
// This method is useful mainly for regenerating profiles.
func (b *Backend) SetupMany(snaps []*snap.Info, confinement func(snapName string) interfaces.ConfinementOptions, repo *interfaces.Repository, tm timings.Measurer) []error {
	var allChangedPaths, allUnchangedPaths, allRemovedPaths []string
	var fallback bool
	for _, snapInfo := range snaps {
		opts := confinement(snapInfo.InstanceName())
		prof, err := b.prepareProfiles(snapInfo, opts, repo)
		if err != nil {
			fallback = true
			break
		}
		allChangedPaths = append(allChangedPaths, prof.changed...)
		allUnchangedPaths = append(allUnchangedPaths, prof.unchanged...)
		allRemovedPaths = append(allRemovedPaths, prof.removed...)
	}

	if !fallback {
		aaFlags := skipReadCache | conserveCPU
		if b.preseed {
			aaFlags |= skipKernelLoad
		}
		var errReloadChanged error
		timings.Run(tm, "load-profiles[changed-many]", fmt.Sprintf("load changed security profiles of %d snaps", len(snaps)), func(nesttm timings.Measurer) {
			errReloadChanged = loadProfiles(allChangedPaths, apparmor_sandbox.CacheDir, aaFlags)
		})

		aaFlags = conserveCPU
		if b.preseed {
			aaFlags |= skipKernelLoad
		}
		var errReloadOther error
		timings.Run(tm, "load-profiles[unchanged-many]", fmt.Sprintf("load unchanged security profiles %d snaps", len(snaps)), func(nesttm timings.Measurer) {
			errReloadOther = loadProfiles(allUnchangedPaths, apparmor_sandbox.CacheDir, aaFlags)
		})

		errUnload := unloadProfiles(allRemovedPaths, apparmor_sandbox.CacheDir)
		if errReloadChanged != nil {
			logger.Noticef("failed to batch-reload changed profiles: %s", errReloadChanged)
			fallback = true
		}
		if errReloadOther != nil {
			logger.Noticef("failed to batch-reload unchanged profiles: %s", errReloadOther)
			fallback = true
		}
		if errUnload != nil {
			logger.Noticef("failed to batch-unload profiles: %s", errUnload)
			fallback = true
		}
	}

	var errors []error
	// if an error was encountered when processing all profiles at once, re-try them one by one
	if fallback {
		for _, snapInfo := range snaps {
			opts := confinement(snapInfo.InstanceName())
			if err := b.Setup(snapInfo, opts, repo, tm); err != nil {
				errors = append(errors, fmt.Errorf("cannot setup profiles for snap %q: %s", snapInfo.InstanceName(), err))
			}
		}
	}
	return errors
}

// Remove removes and unloads apparmor profiles of a given snap.
func (b *Backend) Remove(snapName string) error {
	dir := dirs.SnapAppArmorDir
	globs := profileGlobs(snapName)
	cache := apparmor_sandbox.CacheDir
	_, removed, errEnsure := osutil.EnsureDirStateGlobs(dir, globs, nil)
	// always try to unload affected profiles
	errUnload := unloadProfiles(removed, cache)
	if errEnsure != nil {
		return fmt.Errorf("cannot synchronize security files for snap %q: %s", snapName, errEnsure)
	}
	return errUnload
}

func (b *Backend) RemoveLate(snapName string, rev snap.Revision, typ snap.Type) error {
	logger.Debugf("remove late for snap %v (%s) type %v", snapName, rev, typ)
	if typ != snap.TypeSnapd {
		// late remove is relevant only for snap confine profiles
		return nil
	}

	globs := []string{snapConfineProfileName(snapName, rev)}
	_, removed, errEnsure := osutil.EnsureDirStateGlobs(dirs.SnapAppArmorDir, globs, nil)
	// XXX: unloadProfiles() does not unload profiles from the kernel, but
	// only removes profiles from the cache
	// always try to unload the affected profile
	errUnload := unloadProfiles(removed, apparmor_sandbox.CacheDir)
	if errEnsure != nil {
		return fmt.Errorf("cannot remove security profiles for snap %q (%s): %s", snapName, rev, errEnsure)
	}
	return errUnload
}

var (
	templatePattern    = regexp.MustCompile("(###[A-Z_]+###)")
	coreRuntimePattern = regexp.MustCompile("^core([0-9][0-9])?$")
)

const (
	attachPattern  = "(attach_disconnected,mediate_deleted)"
	attachComplain = "(attach_disconnected,mediate_deleted,complain)"
)

func (b *Backend) deriveContent(spec *Specification, snapInfo *snap.Info, opts interfaces.ConfinementOptions) (content map[string]osutil.FileState) {
	content = make(map[string]osutil.FileState, len(snapInfo.Apps)+len(snapInfo.Hooks)+1)

	// Add profile for each app.
	for _, appInfo := range snapInfo.Apps {
		securityTag := appInfo.SecurityTag()
		addContent(securityTag, snapInfo, appInfo.Name, opts, spec.SnippetForTag(securityTag), content, spec)
	}
	// Add profile for each hook.
	for _, hookInfo := range snapInfo.Hooks {
		securityTag := hookInfo.SecurityTag()
		addContent(securityTag, snapInfo, "hook."+hookInfo.Name, opts, spec.SnippetForTag(securityTag), content, spec)
	}
	// Add profile for snap-update-ns if we have any apps or hooks.
	// If we have neither then we don't have any need to create an executing environment.
	// This applies to, for example, kernel snaps or gadget snaps (unless they have hooks).
	if len(content) > 0 {
		snippets := strings.Join(spec.UpdateNS(), "\n")
		addUpdateNSProfile(snapInfo, snippets, content)
	}

	return content
}

// addUpdateNSProfile adds an apparmor profile for snap-update-ns, tailored to a specific snap.
//
// This profile exists so that snap-update-ns doens't need to carry very wide, open permissions
// that are suitable for poking holes (and writing) in nearly arbitrary places. Instead the profile
// contains just the permissions needed to poke a hole and write to the layout-specific paths.
func addUpdateNSProfile(snapInfo *snap.Info, snippets string, content map[string]osutil.FileState) {
	// Compute the template by injecting special updateNS snippets.
	policy := templatePattern.ReplaceAllStringFunc(updateNSTemplate, func(placeholder string) string {
		switch placeholder {
		case "###SNAP_INSTANCE_NAME###":
			return snapInfo.InstanceName()
		case "###SNIPPETS###":
			if overlayRoot, _ := isRootWritableOverlay(); overlayRoot != "" {
				snippets += strings.Replace(overlayRootSnippet, "###UPPERDIR###", overlayRoot, -1)
			}
			return snippets
		}
		return ""
	})

	// Ensure that the snap-update-ns profile is on disk.
	profileName := nsProfile(snapInfo.InstanceName())
	content[profileName] = &osutil.MemoryFileState{
		Content: []byte(policy),
		Mode:    0644,
	}
}

func addContent(securityTag string, snapInfo *snap.Info, cmdName string, opts interfaces.ConfinementOptions, snippetForTag string, content map[string]osutil.FileState, spec *Specification) {
	// If base is specified and it doesn't match the core snaps (not
	// specifying a base should use the default core policy since in this
	// case, the 'core' snap is used for the runtime), use the base
	// apparmor template, otherwise use the default template.
	var policy string
	if snapInfo.Base != "" && !coreRuntimePattern.MatchString(snapInfo.Base) {
		policy = defaultOtherBaseTemplate
	} else {
		policy = defaultCoreRuntimeTemplate
	}

	ignoreSnippets := false
	// Classic confinement (unless overridden by JailMode) has a dedicated
	// permissive template that applies a strict, but very open, policy.
	if opts.Classic && !opts.JailMode {
		policy = classicTemplate
		ignoreSnippets = true
	}
	// If a snap is in devmode (or is using classic confinement) then make the
	// profile non-enforcing where violations are logged but not denied.
	// This is also done for classic so that no confinement applies. Just in
	// case the profile we start with is not permissive enough.
	if (opts.DevMode || opts.Classic) && !opts.JailMode {
		policy = strings.Replace(policy, attachPattern, attachComplain, -1)
	}
	policy = templatePattern.ReplaceAllStringFunc(policy, func(placeholder string) string {
		switch placeholder {
		case "###VAR###":
			return templateVariables(snapInfo, securityTag, cmdName)
		case "###PROFILEATTACH###":
			return fmt.Sprintf("profile \"%s\"", securityTag)
		case "###CHANGEPROFILE_RULE###":
			features, _ := parserFeatures()
			for _, f := range features {
				if f == "unsafe" {
					return "change_profile unsafe /**,"
				}
			}
			return "change_profile,"
		case "###SNIPPETS###":
			var tagSnippets string
			if opts.Classic && opts.JailMode {
				// Add a special internal snippet for snaps using classic confinement
				// and jailmode together. This snippet provides access to the core snap
				// so that the dynamic linker and shared libraries can be used.
				tagSnippets = classicJailmodeSnippet + "\n" + snippetForTag
			} else if ignoreSnippets {
				// When classic confinement template is in effect we are
				// ignoring all apparmor snippets as they may conflict with the
				// super-broad template we are starting with.
			} else {
				// Check if NFS is mounted at or under $HOME. Because NFS is not
				// transparent to apparmor we must alter the profile to counter that and
				// allow access to SNAP_USER_* files.
				tagSnippets = snippetForTag
				if nfs, _ := isHomeUsingNFS(); nfs {
					tagSnippets += nfsSnippet
				}

				if overlayRoot, _ := isRootWritableOverlay(); overlayRoot != "" {
					snippet := strings.Replace(overlayRootSnippet, "###UPPERDIR###", overlayRoot, -1)
					tagSnippets += snippet
				}
			}

			if !ignoreSnippets {
				// For policy with snippets that request
				// suppression of 'ptrace (trace)' denials, add
				// the suppression rule unless another
				// interface said it uses them.
				if spec.SuppressPtraceTrace() && !spec.UsesPtraceTrace() {
					tagSnippets += ptraceTraceDenySnippet
				}

				// Deny the sys_module capability unless it has been explicitly
				// requested
				if spec.SuppressSysModuleCapability() && !spec.UsesSysModuleCapability() {
					tagSnippets += sysModuleCapabilityDenySnippet
				}

				// Use 'ix' rules in the home interface unless an
				// interface asked to suppress them
				repl := "ix"
				if spec.SuppressHomeIx() {
					repl = ""
				}
				tagSnippets = strings.Replace(tagSnippets, "###HOME_IX###", repl, -1)

				// Conditionally add privilege dropping policy
				if len(snapInfo.SystemUsernames) > 0 {
					tagSnippets += privDropAndChownRules
				}
			}

			return tagSnippets
		}
		return ""
	})

	content[securityTag] = &osutil.MemoryFileState{
		Content: []byte(policy),
		Mode:    0644,
	}
}

// NewSpecification returns a new, empty apparmor specification.
func (b *Backend) NewSpecification() interfaces.Specification {
	return &Specification{}
}

// SandboxFeatures returns the list of apparmor features supported by the kernel.
func (b *Backend) SandboxFeatures() []string {
	if apparmor_sandbox.ProbedLevel() == apparmor_sandbox.Unsupported {
		return nil
	}

	kFeatures, _ := kernelFeatures()
	pFeatures, _ := parserFeatures()
	tags := make([]string, 0, len(kFeatures)+len(pFeatures))
	for _, feature := range kFeatures {
		// Prepend "kernel:" to apparmor kernel features to namespace them and
		// allow us to introduce our own tags later.
		tags = append(tags, "kernel:"+feature)
	}

	for _, feature := range pFeatures {
		// Prepend "parser:" to apparmor kernel features to namespace
		// them and allow us to introduce our own tags later.
		tags = append(tags, "parser:"+feature)
	}

	level := "full"
	policy := "default"
	if apparmor_sandbox.ProbedLevel() == apparmor_sandbox.Partial {
		level = "partial"
	}
	tags = append(tags, fmt.Sprintf("support-level:%s", level))
	tags = append(tags, fmt.Sprintf("policy:%s", policy))

	return tags
}

// MockIsHomeUsingNFS mocks the real implementation of osutil.IsHomeUsingNFS.
// This is exported so that other packages that indirectly interact with AppArmor backend
// can mock isHomeUsingNFS.
func MockIsHomeUsingNFS(new func() (bool, error)) (restore func()) {
	old := isHomeUsingNFS
	isHomeUsingNFS = new
	return func() {
		isHomeUsingNFS = old
	}
}
