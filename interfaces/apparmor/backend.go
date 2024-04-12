// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2024 Canonical Ltd
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
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/snapcore/snapd/dirs"
	snapd_features "github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
	apparmor_sandbox "github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snapdenv"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/timings"
)

var (
	procSelfExe           = "/proc/self/exe"
	isRootWritableOverlay = osutil.IsRootWritableOverlay
	kernelFeatures        = apparmor_sandbox.KernelFeatures
	parserFeatures        = apparmor_sandbox.ParserFeatures
	loadProfiles          = apparmor_sandbox.LoadProfiles
	removeCachedProfiles  = apparmor_sandbox.RemoveCachedProfiles

	// make sure that apparmor profile fulfills the late discarding backend
	// interface
	_ interfaces.SecurityBackendDiscardingLate = (*Backend)(nil)
)

// Backend is responsible for maintaining apparmor profiles for snaps and parts of snapd.
type Backend struct {
	preseed bool

	coreSnap  *snap.Info
	snapdSnap *snap.Info
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

	if opts != nil {
		b.coreSnap = opts.CoreSnapInfo
		b.snapdSnap = opts.SnapdSnapInfo
	}
	// NOTE: It would be nice if we could also generate the profile for
	// snap-confine executing from the core snap, right here, and not have to
	// do this in the Setup function below. I sadly don't think this is
	// possible because snapd must be able to install a new core and only at
	// that moment generate it.

	// Check the /proc/self/exe symlink, this is needed below but we want to
	// fail early if this fails for whatever reason.
	exe, err := os.Readlink(procSelfExe)
	if err != nil {
		return fmt.Errorf("cannot read %s: %s", procSelfExe, err)
	}

	if _, err := apparmor_sandbox.SetupSnapConfineSnippets(); err != nil {
		return err
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
	profilePath := apparmor_sandbox.SnapConfineDistroProfilePath()
	if profilePath == "" {
		// XXX: is profile mandatory on some distros?

		// There is no AppArmor profile for snap-confine, quite likely
		// AppArmor support is enabled in the kernel and relevant
		// userspace tools exist, but snap-confine was built without it,
		// nothing we need to update then.
		logger.Noticef("snap-confine apparmor profile is absent, nothing to update")
		return nil
	}

	aaFlags := apparmor_sandbox.SkipReadCache
	if b.preseed {
		aaFlags |= apparmor_sandbox.SkipKernelLoad
	}

	if err := loadProfiles([]string{profilePath}, apparmor_sandbox.SystemCacheDir, aaFlags); err != nil {
		// When we cannot reload the profile then let's remove the generated
		// policy. Maybe we have caused the problem so it's better to let other
		// things work.
		apparmor_sandbox.RemoveSnapConfineSnippets()
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
	vanillaProfileText, err := os.ReadFile(vanillaProfilePath)
	if os.IsNotExist(err) {
		vanillaProfilePath = filepath.Join(info.MountDir(), "/etc/apparmor.d/usr.lib.snapd.snap-confine")
		vanillaProfileText, err = os.ReadFile(vanillaProfilePath)
	}
	if err != nil {
		return "", "", nil, fmt.Errorf("cannot open apparmor profile for vanilla snap-confine: %s", err)
	}

	// Replace the path to vanilla snap-confine with the path to the mounted snap-confine from core.
	snapConfineInCore := filepath.Join(info.MountDir(), "usr/lib/snapd/snap-confine")
	patchedProfileText := bytes.Replace(
		vanillaProfileText, []byte("/usr/lib/snapd/snap-confine"), []byte(snapConfineInCore), -1)

	// Replace the path to the vanilla snap-confine apparmor snippets
	patchedProfileText = bytes.Replace(
		patchedProfileText, []byte("/var/lib/snapd/apparmor/snap-confine"), []byte(apparmor_sandbox.SnapConfineAppArmorDir), -1)

	// To support non standard homedirs we currently use the home.d tunables, which are
	// written to the system apparmor directory. However snapd vendors its own apparmor, which
	// uses the readonly filesystem, which we cannot modify with our own snippets. So we force
	// include the home.d tunables from /etc if necessary.
	// We should be safely able to use "#include if exists" as the vendored apparmor supports this.
	// XXX: Replace include home tunables until we have a better solution
	features, _ := parserFeatures()
	if strutil.ListContains(features, "snapd-internal") {
		patchedProfileText = bytes.Replace(
			patchedProfileText,
			[]byte("#include <tunables/global>"),
			[]byte("#include <tunables/global>\n#include if exists \"/etc/apparmor.d/tunables/home.d/\""), -1)
	}

	// Also replace the test providing access to verbatim
	// /usr/lib/snapd/snap-confine, which is necessary because to execute snaps
	// from strict snaps, we need to be able read and map
	// /usr/lib/snapd/snap-confine from inside the strict snap mount namespace,
	// even though /usr/lib/snapd/snap-confine from inside the strict snap mount
	// namespace is actually a bind mount to the "snapConfineInCore"
	patchedProfileText = bytes.Replace(
		patchedProfileText, []byte("#@VERBATIM_LIBEXECDIR_SNAP_CONFINE@"), []byte("/usr/lib/snapd/snap-confine"), -1)

	// We need to add a unique prefix that can never collide with a
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
	if err := os.MkdirAll(apparmor_sandbox.SnapConfineAppArmorDir, 0755); err != nil {
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
		// XXX: because remote file system workaround is handled separately the
		// same correct snap-confine profile may need to be re-loaded. This is
		// because the profile contains include directives and those load a
		// second file that has changed outside of the scope of EnsureDirState.
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

	var aaFlags apparmor_sandbox.AaParserFlags
	if b.preseed {
		aaFlags = apparmor_sandbox.SkipKernelLoad
	}
	errReload := loadProfiles(pathnames, cache, aaFlags)
	errRemoveCached := removeCachedProfiles(removed, cache)
	if errEnsure != nil {
		return fmt.Errorf("cannot synchronize snap-confine apparmor profile: %s", errEnsure)
	}
	if errReload != nil {
		return fmt.Errorf("cannot reload snap-confine apparmor profile: %s", errReload)
	}
	if errRemoveCached != nil {
		return fmt.Errorf("cannot remove cached snap-confine apparmor profile: %s", errReload)
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
	return append(interfaces.SecurityTagGlobs(snapName), nsProfile(snapName))
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

func (b *Backend) prepareProfiles(appSet *interfaces.SnapAppSet, opts interfaces.ConfinementOptions, repo *interfaces.Repository) (prof *profilePathsResults, err error) {
	snapName := appSet.InstanceName()
	spec, err := repo.SnapSpecification(b.Name(), appSet)
	if err != nil {
		return nil, fmt.Errorf("cannot obtain apparmor specification for snap %q: %s", snapName, err)
	}

	snapInfo := appSet.Info()

	// Add snippets for parallel snap installation mapping
	spec.(*Specification).AddOvername(snapInfo)

	// Add snippets derived from the layout definition.
	spec.(*Specification).AddLayout(appSet)

	// Add additional mount layouts rules for the snap.
	spec.(*Specification).AddExtraLayouts(snapInfo, opts.ExtraLayouts)

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
	content := b.deriveContent(spec.(*Specification), appSet, opts)

	dir := dirs.SnapAppArmorDir
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("cannot create directory for apparmor profiles %q: %s", dir, err)
	}

	globs := profileGlobs(snapInfo.InstanceName())

	changed, removedPaths, errEnsure := osutil.EnsureDirStateGlobs(dir, globs, content)
	// XXX: in the old code this error was reported late, after doing load/removeCached.
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
func (b *Backend) Setup(appSet *interfaces.SnapAppSet, opts interfaces.ConfinementOptions, repo *interfaces.Repository, tm timings.Measurer) error {
	prof, err := b.prepareProfiles(appSet, opts, repo)
	if err != nil {
		return err
	}

	snapInfo := appSet.Info()

	// Load all changed profiles with a flag that asks apparmor to skip reading
	// the cache (since we know those changed for sure).  This allows us to
	// work despite time being wrong (e.g. in the past). For more details see
	// https://forum.snapcraft.io/t/apparmor-profile-caching/1268/18
	var errReloadChanged error
	aaFlags := apparmor_sandbox.SkipReadCache
	if b.preseed {
		aaFlags |= apparmor_sandbox.SkipKernelLoad
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
		aaFlags |= apparmor_sandbox.SkipKernelLoad
	}
	timings.Run(tm, "load-profiles[unchanged]", fmt.Sprintf("load unchanged security profiles of snap %q", snapInfo.InstanceName()), func(nesttm timings.Measurer) {
		errReloadOther = loadProfiles(prof.unchanged, apparmor_sandbox.CacheDir, aaFlags)
	})
	errRemoveCached := removeCachedProfiles(prof.removed, apparmor_sandbox.CacheDir)
	if errReloadChanged != nil {
		return errReloadChanged
	}
	if errReloadOther != nil {
		return errReloadOther
	}
	return errRemoveCached
}

// SetupMany creates and loads apparmor profiles for multiple snaps.
// The snaps can be in developer mode to make security violations non-fatal to
// the offending application process.
// SetupMany tries to recreate all profiles without interrupting on errors, but
// collects and returns them all.
//
// This method is useful mainly for regenerating profiles.
func (b *Backend) SetupMany(appSets []*interfaces.SnapAppSet, confinement func(snapName string) interfaces.ConfinementOptions, repo *interfaces.Repository, tm timings.Measurer) []error {
	var allChangedPaths, allUnchangedPaths, allRemovedPaths []string
	var fallback bool
	for _, set := range appSets {
		opts := confinement(set.InstanceName())
		prof, err := b.prepareProfiles(set, opts, repo)
		if err != nil {
			fallback = true
			break
		}
		allChangedPaths = append(allChangedPaths, prof.changed...)
		allUnchangedPaths = append(allUnchangedPaths, prof.unchanged...)
		allRemovedPaths = append(allRemovedPaths, prof.removed...)
	}

	if !fallback {
		aaFlags := apparmor_sandbox.SkipReadCache | apparmor_sandbox.ConserveCPU
		if b.preseed {
			aaFlags |= apparmor_sandbox.SkipKernelLoad
		}
		var errReloadChanged error
		timings.Run(tm, "load-profiles[changed-many]", fmt.Sprintf("load changed security profiles of %d snaps", len(appSets)), func(nesttm timings.Measurer) {
			errReloadChanged = loadProfiles(allChangedPaths, apparmor_sandbox.CacheDir, aaFlags)
		})

		aaFlags = apparmor_sandbox.ConserveCPU
		if b.preseed {
			aaFlags |= apparmor_sandbox.SkipKernelLoad
		}
		var errReloadOther error
		timings.Run(tm, "load-profiles[unchanged-many]", fmt.Sprintf("load unchanged security profiles %d snaps", len(appSets)), func(nesttm timings.Measurer) {
			errReloadOther = loadProfiles(allUnchangedPaths, apparmor_sandbox.CacheDir, aaFlags)
		})

		errRemoveCached := removeCachedProfiles(allRemovedPaths, apparmor_sandbox.CacheDir)
		if errReloadChanged != nil {
			logger.Noticef("failed to batch-reload changed profiles: %s", errReloadChanged)
			fallback = true
		}
		if errReloadOther != nil {
			logger.Noticef("failed to batch-reload unchanged profiles: %s", errReloadOther)
			fallback = true
		}
		if errRemoveCached != nil {
			logger.Noticef("failed to batch-remove cached profiles: %s", errRemoveCached)
			fallback = true
		}
	}

	var errors []error
	// if an error was encountered when processing all profiles at once, re-try them one by one
	if fallback {
		for _, set := range appSets {
			instanceName := set.InstanceName()
			opts := confinement(instanceName)
			if err := b.Setup(set, opts, repo, tm); err != nil {
				errors = append(errors, fmt.Errorf("cannot setup profiles for snap %q: %s", instanceName, err))
			}
		}
	}
	return errors
}

// Removes all AppArmor profiles from disk but does not unload them from the
// kernel - currently it is not possible to ensure that all services of a snap
// are stopped and so it is not possible to safely unload the profile for a snap
// from the kernel and so we only remove it from the cache on disk
func RemoveAllSnapAppArmorProfiles() error {
	dir := dirs.SnapAppArmorDir
	globs := []string{"snap.*", "snap-update-ns.*", "snap-confine.*"}
	cache := apparmor_sandbox.CacheDir
	_, removed, errEnsure := osutil.EnsureDirStateGlobs(dir, globs, nil)
	errRemoveCached := removeCachedProfiles(removed, cache)
	switch {
	case errEnsure != nil && errRemoveCached != nil:
		return fmt.Errorf("cannot remove apparmor profiles: %s (and also %s)", errEnsure, errRemoveCached)
	case errEnsure != nil:
		return fmt.Errorf("cannot remove apparmor profiles: %s", errEnsure)
	case errRemoveCached != nil:
		return errRemoveCached
	default:
		return nil
	}
}

// Remove removes the apparmor profiles of a given snap from disk and the cache.
func (b *Backend) Remove(snapName string) error {
	dir := dirs.SnapAppArmorDir
	globs := profileGlobs(snapName)
	cache := apparmor_sandbox.CacheDir
	_, removed, errEnsure := osutil.EnsureDirStateGlobs(dir, globs, nil)
	// always try to remove affected profiles from the cache
	errRemoveCached := removeCachedProfiles(removed, cache)
	if errEnsure != nil {
		return fmt.Errorf("cannot synchronize security files for snap %q: %s", snapName, errEnsure)
	}
	return errRemoveCached
}

func (b *Backend) RemoveLate(snapName string, rev snap.Revision, typ snap.Type) error {
	logger.Debugf("remove late for snap %v (%s) type %v", snapName, rev, typ)
	if typ != snap.TypeSnapd {
		// late remove is relevant only for snap confine profiles
		return nil
	}

	globs := []string{snapConfineProfileName(snapName, rev)}
	_, removed, errEnsure := osutil.EnsureDirStateGlobs(dirs.SnapAppArmorDir, globs, nil)
	// XXX: we should also try and unload the profile from the kernel
	// instead of just removing it from the cache but currently it is not
	// possible to ensure all snap services are stopped at this time and so
	// it is not safe to unload the profile
	errRemoveCached := removeCachedProfiles(removed, apparmor_sandbox.CacheDir)
	if errEnsure != nil {
		return fmt.Errorf("cannot remove security profiles for snap %q (%s): %s", snapName, rev, errEnsure)
	}
	return errRemoveCached
}

var (
	templatePattern    = regexp.MustCompile("(###[A-Z_]+###)")
	coreRuntimePattern = regexp.MustCompile("^core([0-9][0-9])?$")
)

func (b *Backend) deriveContent(spec *Specification, appSet *interfaces.SnapAppSet, opts interfaces.ConfinementOptions) (content map[string]osutil.FileState) {
	runnables := appSet.Runnables()
	content = make(map[string]osutil.FileState, len(runnables))
	snapInfo := appSet.Info()

	// Add profile for apps and hooks.
	for _, r := range runnables {
		b.addContent(r.SecurityTag, snapInfo, r.CommandName, opts, spec.SnippetForTag(r.SecurityTag), content, spec)
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
				snippets += strings.Replace(apparmor_sandbox.OverlayRootSnippet, "###UPPERDIR###", overlayRoot, -1)
			}
			return snippets
		case "###INCLUDE_SYSTEM_TUNABLES_HOME_D_WITH_VENDORED_APPARMOR###":
			// XXX: refactor this so that we don't have to duplicate this part.
			// TODO: rewrite this whole mess with go templates.
			features, _ := parserFeatures()
			if strutil.ListContains(features, "snapd-internal") {
				return `#include if exists "/etc/apparmor.d/tunables/home.d"`
			}
			return ""
		default:
			if snapdenv.Testing() || osutil.IsTestBinary() {
				panic(fmt.Sprintf("cannot expand snippet for pattern %q", placeholder))
			}
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

func (b *Backend) addContent(securityTag string, snapInfo *snap.Info, cmdName string, opts interfaces.ConfinementOptions, snippetForTag string, content map[string]osutil.FileState, spec *Specification) {
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
	policy = templatePattern.ReplaceAllStringFunc(policy, func(placeholder string) string {
		switch placeholder {
		case "###DEVMODE_SNAP_CONFINE###":
			if !opts.DevMode {
				// nothing to add if we are not in devmode
				return ""
			}

			// otherwise we need to generate special policy to allow executing
			// snap-confine from inside a devmode snap

			// TODO: we should deprecate this and drop it in a future release

			// assumes coreSnapInfo is not nil
			coreProfileTarget := func() string {
				return fmt.Sprintf("/snap/core/%s/usr/lib/snapd/snap-confine", b.coreSnap.SnapRevision().String())
			}

			// assumes snapdSnapInfo is not nil
			snapdProfileTarget := func() string {
				return fmt.Sprintf("/snap/snapd/%s/usr/lib/snapd/snap-confine", b.snapdSnap.SnapRevision().String())
			}

			// There are 3 main apparmor exec transition rules we need to
			// generate:
			// * exec( /usr/lib/snapd/snap-confine ... )
			// * exec( /snap/snapd/<rev>/usr/lib/snapd/snap-confine ... )
			// * exec( /snap/core/<rev>/usr/lib/snapd/snap-confine ... )

			// The latter two can always transition to their respective
			// revisioned profiles unambiguously if each snap is installed.

			// The former rule for /usr/lib/snapd/snap-confine however is
			// more tricky. First, we can say that if only the snapd snap is
			// installed, to just transition to that profile and be done. If
			// just the core snap is installed, then we can deduce this
			// system is either UC16 or a classic one, in both cases though
			// we have /usr/lib/snapd/snap-confine defined as the profile to
			// transition to.
			// If both snaps are installed however, then we need to branch
			// and pick a profile that exists, we can't just arbitrarily
			// pick one profile because not all profiles will exist on all
			// systems actually, for example the snap-confine profile from
			// the core snap will not be generated/installed on UC18+. We
			// can simplify the logic however by realizing that no matter
			// the relative version numbers of snapd and core, when
			// executing a snap with base other than core (i.e. base core18
			// or core20), the snapd snap's version of snap-confine will
			// always be used for various reasons. This is also true for
			// base: core snaps, but only on non-classic systems. So we
			// essentially say that /usr/lib/snapd/snap-confine always
			// transitions to the snapd snap profile if the base is not
			// core or if the system is not classic. If the base is core and
			// the system is classic, then the core snap profile will be
			// used.

			usrLibSnapdConfineTransitionTarget := ""
			switch {
			case b.coreSnap != nil && b.snapdSnap == nil:
				// only core snap - use /usr/lib/snapd/snap-confine always
				usrLibSnapdConfineTransitionTarget = "/usr/lib/snapd/snap-confine"
			case b.snapdSnap != nil && b.coreSnap == nil:
				// only snapd snap - use snapd snap version
				usrLibSnapdConfineTransitionTarget = snapdProfileTarget()
			case b.snapdSnap != nil && b.coreSnap != nil:
				// both are installed - need to check which one to use
				// note that a base of "core" is represented by base == "" for
				// historical reasons
				if release.OnClassic && snapInfo.Base == "" {
					// use the core snap as the target only if we are on
					// classic and the base is core
					usrLibSnapdConfineTransitionTarget = coreProfileTarget()
				} else {
					// otherwise always use snapd
					usrLibSnapdConfineTransitionTarget = snapdProfileTarget()
				}

			default:
				// neither of the snaps are installed

				// TODO: this panic is unfortunate, but we don't have time
				// to do any better for this security release
				// It is actually important that we panic here, the only
				// known circumstance where this happens is when we are
				// seeding during first boot of UC16 with a very new core
				// snap (i.e. with the security fix of 2.54.3) and also have
				// a devmode confined snap in the seed to prepare. In this
				// situation, when we panic(), we force snapd to exit, and
				// systemd will restart us and we actually recover the
				// initial seed change and continue on. This code will be
				// removed/adapted before it is merged to the main branch,
				// it is only meant to exist on the security release branch.
				msg := fmt.Sprintf("neither snapd nor core snap available while preparing apparmor profile for devmode snap %s, panicking to restart snapd to continue seeding", snapInfo.InstanceName())
				panic(msg)
			}

			// We use Pxr for all these rules since the snap-confine profile
			// is not a child profile of the devmode complain profile we are
			// generating right now.
			usrLibSnapdConfineTransitionRule := fmt.Sprintf("/usr/lib/snapd/snap-confine Pxr -> %s,\n", usrLibSnapdConfineTransitionTarget)

			coreSnapConfineSnippet := ""
			if b.coreSnap != nil {
				coreSnapConfineSnippet = fmt.Sprintf("/snap/core/*/usr/lib/snapd/snap-confine Pxr -> %s,\n", coreProfileTarget())
			}

			snapdSnapConfineSnippet := ""
			if b.snapdSnap != nil {
				snapdSnapConfineSnippet = fmt.Sprintf("/snap/snapd/*/usr/lib/snapd/snap-confine Pxr -> %s,\n", snapdProfileTarget())
			}

			nonBaseCoreTransitionSnippet := coreSnapConfineSnippet + "\n" + snapdSnapConfineSnippet

			// include both rules for the core snap and the snapd snap since
			// we can't know which one will be used at runtime (for example
			// SNAP_REEXEC could be set which affects which one is used)
			return fmt.Sprintf(`
  # allow executing the snap command from either the rootfs (for base: core) or
  # from the system snaps (all other bases) - this is very specifically only to
  # enable proper apparmor profile transition to snap-confine below, if we don't
  # include these exec rules, then when executing the snap command, apparmor 
  # will create a new, unique sub-profile which then cannot be transitioned from
  # to the actual snap-confine profile
  /usr/bin/snap ixr,
  /snap/{snapd,core}/*/usr/bin/snap ixr,

  # allow transitioning to snap-confine to support executing strict snaps from
  # inside devmode confined snaps

  # this first rule is to handle the case of exec()ing 
  # /usr/lib/snapd/snap-confine directly, the profile we transition to depends
  # on whether we are classic or not, what snaps (snapd or core) are installed
  # and also whether this snap is a base: core snap or a differently based snap.
  # see the comment in interfaces/backend/apparmor.go where this snippet is
  # generated for the full context
  %[1]s

  # the second (and possibly third if both core and snapd are installed) rule is
  # to handle direct exec() of snap-confine from the respective snaps directly, 
  # this happens mostly on non-core based snaps, wherein the base snap has a 
  # symlink from /usr/bin/snap -> /snap/snapd/current/usr/bin/snap, which makes
  # the snap command execute snap-confine directly from the associated system 
  # snap in /snap/{snapd,core}/<rev>/usr/lib/snapd/snap-confine
  %[2]s
`, usrLibSnapdConfineTransitionRule, nonBaseCoreTransitionSnippet)

		case "###INCLUDE_IF_EXISTS_SNAP_TUNING###":
			features, _ := parserFeatures()
			if strutil.ListContains(features, "include-if-exists") {
				return `#include if exists "/var/lib/snapd/apparmor/snap-tuning"`
			}
			return ""
		// XXX: Remove this when we have a better solution to including the system
		// tunables. See snapConfineFromSnapProfile() for a more detailed explanation.
		case "###INCLUDE_SYSTEM_TUNABLES_HOME_D_WITH_VENDORED_APPARMOR###":
			features, _ := parserFeatures()
			if strutil.ListContains(features, "snapd-internal") {
				return `#include if exists "/etc/apparmor.d/tunables/home.d"`
			}
			return ""
		case "###VAR###":
			return templateVariables(snapInfo, securityTag, cmdName)
		case "###PROFILEATTACH###":
			return fmt.Sprintf("profile \"%s\"", securityTag)
		case "###FLAGS###":
			// default flags
			flags := []string{"attach_disconnected", "mediate_deleted"}
			if spec.Unconfined() == UnconfinedEnabled {
				// need both parser and kernel support for unconfined
				pfeatures, _ := parserFeatures()
				kfeatures, _ := kernelFeatures()
				if strutil.ListContains(pfeatures, "unconfined") &&
					strutil.ListContains(kfeatures, "policy:unconfined_restrictions") {
					flags = append(flags, "unconfined")
				}
			}
			// If a snap is in devmode (or is using classic confinement) then make the
			// profile non-enforcing where violations are logged but not denied.
			// This is also done for classic so that no confinement applies. Just in
			// case the profile we start with is not permissive enough.
			if (opts.DevMode || opts.Classic) && !opts.JailMode {
				if !strutil.ListContains(flags, "unconfined") {
					// Profile modes unconfined and complain
					// conflict with each other and are
					// rejected by the parser, in any case
					// this is fine since we already
					// requested unconfined based on the
					// spec and complain would no enforce
					// any rules anyway.
					flags = append(flags, "complain")
				}
			}
			if len(flags) > 0 {
				return "flags=(" + strings.Join(flags, ",") + ")"
			} else {
				return ""
			}
		case "###PYCACHEDENY###":
			if spec.SuppressPycacheDeny() {
				return ""
			}
			return pycacheDenySnippet
		case "###CHANGEPROFILE_RULE###":
			features, _ := parserFeatures()
			if strutil.ListContains(features, "unsafe") {
				return "change_profile unsafe /**,"
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
				// Check if a remote file system is mounted at or under $HOME.
				// Because some file systems, like NFS, are not transparent to
				// apparmor we must alter the profile to counter that and allow
				// access to SNAP_USER_* files.
				tagSnippets = snippetForTag
				if isRemote, _ := osutil.IsHomeUsingRemoteFS(); isRemote {
					tagSnippets += apparmor_sandbox.RemoteFSSnippet
				}

				if overlayRoot, _ := isRootWritableOverlay(); overlayRoot != "" {
					snippet := strings.Replace(apparmor_sandbox.OverlayRootSnippet, "###UPPERDIR###", overlayRoot, -1)
					tagSnippets += snippet
				}

				// Add core specific snippets when not on classic
				if !release.OnClassic {
					tagSnippets += coreSnippet
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

				// Use prompt prefix if prompting is supported and enabled
				repl = ""
				if snapd_features.AppArmorPrompting.IsEnabled() {
					// If prompting flag not set, no change in behavior
					if snapd_features.AppArmorPrompting.IsSupported() {
						// Prompting support requires apparmor kernel and parser
						// features, and these are only checked once during
						// startup, so checking IsSupported() will be consistent
						// within a given snapd run.
						repl = "prompt "
					}
				}
				tagSnippets = strings.Replace(tagSnippets, "###PROMPT###", repl, -1)

				// Conditionally add privilege dropping policy
				if len(snapInfo.SystemUsernames) > 0 {
					tagSnippets += privDropAndChownRules
				}
			}

			return tagSnippets
		default:
			if snapdenv.Testing() || osutil.IsTestBinary() {
				panic(fmt.Sprintf("cannot expand snippet for pattern %q", placeholder))
			} else {
				logger.Noticef("WARNING: cannto expand snippet for pattern %q", placeholder)
			}
		}
		return ""
	})

	content[securityTag] = &osutil.MemoryFileState{
		Content: []byte(policy),
		Mode:    0644,
	}
}

// NewSpecification returns a new, empty apparmor specification.
func (b *Backend) NewSpecification(appSet *interfaces.SnapAppSet) interfaces.Specification {
	return &Specification{appSet: appSet}
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
