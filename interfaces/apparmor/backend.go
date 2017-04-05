// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
)

// Backend is responsible for maintaining apparmor profiles for ubuntu-core-launcher.
type Backend struct{}

// Name returns the name of the backend.
func (b *Backend) Name() interfaces.SecuritySystem {
	return interfaces.SecurityAppArmor
}

func setupHostSnapConfineApparmorForReexec(snapInfo *snap.Info) error {
	// cleanup old
	apparmorProfilePathPattern := strings.Replace(filepath.Join(dirs.SnapMountDir, "/core/*/usr/lib/snapd/snap-confine"), "/", ".", -1)[1:]

	glob, err := filepath.Glob(filepath.Join(dirs.SystemApparmorDir, apparmorProfilePathPattern))
	if err != nil {
		return err
	}

	for _, path := range glob {
		snapConfineInCore := "/" + strings.Replace(filepath.Base(path), ".", "/", -1)
		if osutil.FileExists(snapConfineInCore) {
			continue
		}

		// not using apparmor.UnloadProfile() because it uses a
		// different cachedir
		if output, err := exec.Command("apparmor_parser", "-R", filepath.Base(path)).CombinedOutput(); err != nil {
			logger.Noticef("cannot unload apparmor profile %s: %v", filepath.Base(path), osutil.OutputErr(output, err))
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := os.Remove(filepath.Join(dirs.SystemApparmorCacheDir, filepath.Base(path))); err != nil && !os.IsNotExist(err) {
			return err
		}
	}

	// add new
	coreRoot := snapInfo.MountDir()
	snapConfineInCore := filepath.Join(coreRoot, "usr/lib/snapd/snap-confine")
	apparmorProfilePath := filepath.Join(dirs.SystemApparmorDir, strings.Replace(snapConfineInCore[1:], "/", ".", -1))

	apparmorProfile, err := ioutil.ReadFile(filepath.Join(coreRoot, "/etc/apparmor.d/usr.lib.snapd.snap-confine"))
	if err != nil {
		return err
	}
	apparmorProfileForCore := strings.Replace(string(apparmorProfile), "/usr/lib/snapd/snap-confine", snapConfineInCore, -1)

	// /etc/apparmor.d is read/write OnClassic, so write out the
	// new core's profile there
	if err := osutil.AtomicWriteFile(apparmorProfilePath, []byte(apparmorProfileForCore), 0644, 0); err != nil {
		return err
	}

	// not using apparmor.LoadProfile() because it uses a different cachedir
	if output, err := exec.Command("apparmor_parser", "--replace", "--write-cache", apparmorProfilePath, "--cache-loc", dirs.SystemApparmorCacheDir).CombinedOutput(); err != nil {
		return fmt.Errorf("cannot replace snap-confine apparmor profile: %v", osutil.OutputErr(output, err))
	}

	return nil
}

// Setup creates and loads apparmor profiles specific to a given snap.
// The snap can be in developer mode to make security violations non-fatal to
// the offending application process.
//
// This method should be called after changing plug, slots, connections between
// them or application present in the snap.
func (b *Backend) Setup(snapInfo *snap.Info, opts interfaces.ConfinementOptions, repo *interfaces.Repository) error {
	snapName := snapInfo.Name()
	spec, err := repo.SnapSpecification(b.Name(), snapName)
	if err != nil {
		return fmt.Errorf("cannot obtain apparmor specification for snap %q: %s", snapName, err)
	}

	// core on classic is special
	if snapName == "core" && release.OnClassic && !release.ReleaseInfo.ForceDevMode() {
		if err := setupHostSnapConfineApparmorForReexec(snapInfo); err != nil {
			logger.Noticef("cannot create host snap-confe apparmor configuration: %s", err)
		}
	}

	// Get the files that this snap should have
	content, err := b.deriveContent(spec.(*Specification), snapInfo, opts)
	if err != nil {
		return fmt.Errorf("cannot obtain expected security files for snap %q: %s", snapName, err)
	}
	glob := interfaces.SecurityTagGlob(snapInfo.Name())
	dir := dirs.SnapAppArmorDir
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("cannot create directory for apparmor profiles %q: %s", dir, err)
	}
	_, removed, errEnsure := osutil.EnsureDirState(dir, glob, content)
	// NOTE: load all profiles instead of just the changed profiles.  We're
	// relying on apparmor cache to make this efficient. This gives us
	// certainty that each call to Setup ends up with working profiles.
	all := make([]string, 0, len(content))
	for name := range content {
		all = append(all, name)
	}
	sort.Strings(all)
	errReload := reloadProfiles(all)
	errUnload := unloadProfiles(removed)
	if errEnsure != nil {
		return fmt.Errorf("cannot synchronize security files for snap %q: %s", snapName, errEnsure)
	}
	if errReload != nil {
		return errReload
	}
	return errUnload
}

// Remove removes and unloads apparmor profiles of a given snap.
func (b *Backend) Remove(snapName string) error {
	glob := interfaces.SecurityTagGlob(snapName)
	_, removed, errEnsure := osutil.EnsureDirState(dirs.SnapAppArmorDir, glob, nil)
	errUnload := unloadProfiles(removed)
	if errEnsure != nil {
		return fmt.Errorf("cannot synchronize security files for snap %q: %s", snapName, errEnsure)
	}
	return errUnload
}

var (
	templatePattern = regexp.MustCompile("(###[A-Z]+###)")
	attachPattern   = regexp.MustCompile(`\(attach_disconnected\)`)
)

const attachComplain = "(attach_disconnected,complain)"

func (b *Backend) deriveContent(spec *Specification, snapInfo *snap.Info, opts interfaces.ConfinementOptions) (content map[string]*osutil.FileState, err error) {
	for _, appInfo := range snapInfo.Apps {
		if content == nil {
			content = make(map[string]*osutil.FileState)
		}
		securityTag := appInfo.SecurityTag()
		addContent(securityTag, snapInfo, opts, spec.SnippetForTag(securityTag), content)
	}

	for _, hookInfo := range snapInfo.Hooks {
		if content == nil {
			content = make(map[string]*osutil.FileState)
		}
		securityTag := hookInfo.SecurityTag()
		addContent(securityTag, snapInfo, opts, spec.SnippetForTag(securityTag), content)
	}

	return content, nil
}

func addContent(securityTag string, snapInfo *snap.Info, opts interfaces.ConfinementOptions, snippetForTag string, content map[string]*osutil.FileState) {
	var policy string
	if opts.Classic && !opts.JailMode {
		policy = classicTemplate
	} else {
		policy = defaultTemplate
	}
	if (opts.DevMode || opts.Classic) && !opts.JailMode {
		policy = attachPattern.ReplaceAllString(policy, attachComplain)
	}
	policy = templatePattern.ReplaceAllStringFunc(policy, func(placeholder string) string {
		switch placeholder {
		case "###VAR###":
			return templateVariables(snapInfo, securityTag)
		case "###PROFILEATTACH###":
			return fmt.Sprintf("profile \"%s\"", securityTag)
		case "###SNIPPETS###":
			var tagSnippets string

			if opts.Classic && opts.JailMode {
				// Add a special internal snippet for snaps using classic confinement
				// and jailmode together. This snippet provides access to the core snap
				// so that the dynamic linker and shared libraries can be used.
				tagSnippets = classicJailmodeSnippet + "\n" + snippetForTag
			} else if opts.Classic && !opts.JailMode {
				// When classic confinement (without jailmode) is in effect we
				// are ignoring all apparmor snippets as they may conflict with
				// the super-broad template we are starting with.
			} else {
				tagSnippets = snippetForTag
			}
			return tagSnippets
		}
		return ""
	})

	content[securityTag] = &osutil.FileState{
		Content: []byte(policy),
		Mode:    0644,
	}
}

func reloadProfiles(profiles []string) error {
	for _, profile := range profiles {
		fname := filepath.Join(dirs.SnapAppArmorDir, profile)
		err := LoadProfile(fname)
		if err != nil {
			return fmt.Errorf("cannot load apparmor profile %q: %s", profile, err)
		}
	}
	return nil
}

func unloadProfiles(profiles []string) error {
	for _, profile := range profiles {
		if err := UnloadProfile(profile); err != nil {
			return fmt.Errorf("cannot unload apparmor profile %q: %s", profile, err)
		}
	}
	return nil
}

func (b *Backend) NewSpecification() interfaces.Specification {
	return &Specification{}
}
