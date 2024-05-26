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

package builtin

import (
	"os"
	"path/filepath"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/mount"
	"github.com/snapcore/snapd/osutil"
)

const appstreamMetadataSummary = `allows access to AppStream metadata`

const appstreamMetadataBaseDeclarationSlots = `
  appstream-metadata:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

// Paths for upstream and collection metadata are defined in the
// AppStream specification:
//
//	https://www.freedesktop.org/software/appstream/docs/
const appstreamMetadataConnectedPlugAppArmor = `
# Description: Allow access to AppStream metadata from the host system

# Allow access to AppStream upstream metadata files
/usr/share/metainfo/{,**} r,
/usr/share/appdata/{,**} r,

# Allow access to AppStream collection metadata
/usr/share/{app-info,swcatalog}/** r,
/var/cache/{app-info,swcatalog}/** r,
/var/lib/{app-info,swcatalog}/** r,

# Apt symlinks the DEP-11 metadata to files in /var/lib/apt/lists
/var/lib/apt/lists/*.yml.gz r,
`

var appstreamMetadataDirs = []string{
	"/usr/share/metainfo",
	"/usr/share/appdata",
	"/usr/share/app-info",
	"/usr/share/swcatalog",
	"/var/cache/app-info",
	"/var/cache/swcatalog",
	"/var/lib/app-info",
	"/var/lib/swcatalog",
	"/var/lib/apt/lists",
}

type appstreamMetadataInterface struct {
	commonInterface
}

func (iface *appstreamMetadataInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	spec.AddSnippet(appstreamMetadataConnectedPlugAppArmor)

	// Generate rules to allow snap-update-ns to do its thing
	emit := spec.AddUpdateNSf
	for _, target := range appstreamMetadataDirs {
		source := "/var/lib/snapd/hostfs" + target
		emit("  # Read-only access to %s\n", target)
		emit("  mount options=(bind) %s/ -> %s/,\n", source, target)
		emit("  remount options=(bind, ro) %s/,\n", target)
		emit("  umount %s/,\n\n", target)
		// Allow constructing writable mimic to mount point We
		// expect three components to already exist: /, /usr,
		// and /usr/share (or equivalents under /var).
		apparmor.GenWritableProfile(emit, target, 3)
	}
	return nil
}

func (iface *appstreamMetadataInterface) MountConnectedPlug(spec *mount.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	for _, dir := range appstreamMetadataDirs {
		dir = filepath.Join(dirs.GlobalRootDir, dir)
		if osutil.IsSymlink(dir) {
			target := mylog.Check2(os.Readlink(dir))
			if err == nil {
				spec.AddMountEntry(osutil.MountEntry{
					Name:    "/var/lib/snapd/hostfs" + dir,
					Dir:     dirs.StripRootDir(dir),
					Options: []string{osutil.XSnapdKindSymlink(), osutil.XSnapdSymlink(target)},
				})
			}
		} else if osutil.IsDirectory(dir) {
			spec.AddMountEntry(osutil.MountEntry{
				Name:    "/var/lib/snapd/hostfs" + dir,
				Dir:     dirs.StripRootDir(dir),
				Options: []string{"bind", "ro"},
			})
		}
	}

	return nil
}

func init() {
	registerIface(&appstreamMetadataInterface{commonInterface{
		name:                 "appstream-metadata",
		summary:              appstreamMetadataSummary,
		implicitOnClassic:    true,
		baseDeclarationSlots: appstreamMetadataBaseDeclarationSlots,
		// affects the plug snap because of mount backend
		affectsPlugOnRefresh: true,
	}})
}
