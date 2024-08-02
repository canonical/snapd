// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/mount"
	"github.com/snapcore/snapd/osutil"
)

const systemPackagesDocSummary = `allows access to documentation of system packages`

const systemPackagesDocBaseDeclarationSlots = `
  system-packages-doc:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const systemPackagesDocConnectedPlugAppArmor = `
# Description: can access documentation of system packages.

/usr/{,local/}share/doc/{,**} r,
/usr/share/cups/doc-root/{,**} r,
/usr/share/gimp/2.0/help/{,**} r,
/usr/share/gtk-doc/{,**} r,
/usr/share/javascript/{jquery,jquery-ui,sphinxdoc}/{,**} r,
/usr/share/libreoffice/help/{,**} r,
/usr/share/sphinx_rtd_theme/{,**} r,
/usr/share/xubuntu-docs/{,**} r,
`

type systemPackagesDocInterface struct {
	commonInterface
}

func (iface *systemPackagesDocInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	spec.AddSnippet(systemPackagesDocConnectedPlugAppArmor)
	emit := spec.AddUpdateNSf
	emit("  # Mount documentation of system packages\n")
	emit("  mount options=(bind) /var/lib/snapd/hostfs/usr/share/doc/ -> /usr/share/doc/,\n")
	emit("  remount options=(bind, ro) /usr/share/doc/,\n")
	emit("  umount /usr/share/doc/,\n")
	emit("  mount options=(bind) /var/lib/snapd/hostfs/usr/local/share/doc/ -> /usr/local/share/doc/,\n")
	emit("  remount options=(bind, ro) /usr/local/share/doc/,\n")
	emit("  umount /usr/local/share/doc/,\n")
	emit("  mount options=(bind) /var/lib/snapd/hostfs/usr/share/cups/doc-root/ -> /usr/share/cups/doc-root/,\n")
	emit("  remount options=(bind, ro) /usr/share/cups/doc-root/,\n")
	emit("  umount /usr/share/cups/doc-root/,\n")
	emit("  mount options=(bind) /var/lib/snapd/hostfs/usr/share/gimp/2.0/help/ -> /usr/share/gimp/2.0/help/,\n")
	emit("  remount options=(bind, ro) /usr/share/gimp/2.0/help/,\n")
	emit("  umount /usr/share/gimp/2.0/help/,\n")
	emit("  mount options=(bind) /var/lib/snapd/hostfs/usr/share/gtk-doc/ -> /usr/share/gtk-doc/,\n")
	emit("  remount options=(bind, ro) /usr/share/gtk-doc/,\n")
	emit("  umount /usr/share/gtk-doc/,\n")
	emit("  mount options=(bind) /var/lib/snapd/hostfs/usr/share/javascript/jquery/ -> /usr/share/javascript/jquery/,\n")
	emit("  remount options=(bind, ro) /usr/share/javascript/jquery/,\n")
	emit("  umount /usr/share/javascript/jquery/,\n")
	emit("  mount options=(bind) /var/lib/snapd/hostfs/usr/share/javascript/jquery-ui/ -> /usr/share/javascript/jquery-ui/,\n")
	emit("  remount options=(bind, ro) /usr/share/javascript/jquery-ui/,\n")
	emit("  umount /usr/share/javascript/jquery-ui/,\n")
	emit("  mount options=(bind) /var/lib/snapd/hostfs/usr/share/javascript/sphinxdoc/ -> /usr/share/javascript/sphinxdoc/,\n")
	emit("  remount options=(bind, ro) /usr/share/javascript/sphinxdoc/,\n")
	emit("  umount /usr/share/javascript/sphinxdoc/,\n")
	emit("  mount options=(bind) /var/lib/snapd/hostfs/usr/share/libreoffice/help/ -> /usr/share/libreoffice/help/,\n")
	emit("  remount options=(bind, ro) /usr/share/libreoffice/help/,\n")
	emit("  umount /usr/share/libreoffice/help/,\n")
	emit("  mount options=(bind) /var/lib/snapd/hostfs/usr/share/sphinx_rtd_theme/ -> /usr/share/sphinx_rtd_theme/,\n")
	emit("  remount options=(bind, ro) /usr/share/sphinx_rtd_theme/,\n")
	emit("  umount /usr/share/sphinx_rtd_theme/,\n")
	emit("  mount options=(bind) /var/lib/snapd/hostfs/usr/share/xubuntu-docs/ -> /usr/share/xubuntu-docs/,\n")
	emit("  remount options=(bind, ro) /usr/share/xubuntu-docs/,\n")
	emit("  umount /usr/share/xubuntu-docs/,\n")
	// The mount targets under /usr/share/ do not necessarily exist in the
	// base image, in which case, we need to create a writable mimic.
	apparmor.GenWritableProfile(emit, "/usr/share/cups/", 3)
	apparmor.GenWritableProfile(emit, "/usr/share/gimp/2.0/", 3)
	apparmor.GenWritableProfile(emit, "/usr/share/javascript/jquery/", 3)
	apparmor.GenWritableProfile(emit, "/usr/share/javascript/jquery-ui/", 3)
	apparmor.GenWritableProfile(emit, "/usr/share/javascript/sphinxdoc/", 3)
	apparmor.GenWritableProfile(emit, "/usr/share/libreoffice/", 3)
	apparmor.GenWritableProfile(emit, "/usr/share/sphinx_rtd_theme/", 3)

	if base := plug.Snap().Base; base == "bare" || base == "test-snapd-base-bare" {
		// The bare snap does not have enough mount points, causing us to create a mimic over /
		// which only works when snap-update-ns is invoked without the sandbox by snapd. When invoked
		// from starting snap via the snap-run -> snap-confine -> snap-update-ns chain, the permissions
		// are not sufficient.
		//
		// In essence, constructing this sort of mimic requires nearly arbitrary writes/mounts at root:
		// See bug comments for details LP:#2044335
		emit(`
  # Writable mimic over / - extra permissions generalized
  "/**" rw,
  mount options=(rbind, rw) "/**" -> "/tmp/.snap/**",
  mount fstype=tmpfs options=(rw) tmpfs -> "/**",
  mount options=(rbind, rw) "/tmp/.snap/**" -> "/**",
  mount options=(bind, rw) "/tmp/.snap/**" -> "/**",
  mount options=(rprivate) -> "/tmp/.snap/**",
  mount options=(rprivate) -> "/**",
  umount "/**",
`)
	}

	return nil
}

func (iface *systemPackagesDocInterface) MountConnectedPlug(spec *mount.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	spec.AddMountEntry(osutil.MountEntry{
		Name:    "/var/lib/snapd/hostfs/usr/share/doc",
		Dir:     "/usr/share/doc",
		Options: []string{"bind", "ro"},
	})
	spec.AddMountEntry(osutil.MountEntry{
		Name:    "/var/lib/snapd/hostfs/usr/local/share/doc",
		Dir:     "/usr/local/share/doc",
		Options: []string{"bind", "ro"},
	})
	spec.AddMountEntry(osutil.MountEntry{
		Name:    "/var/lib/snapd/hostfs/usr/share/cups/doc-root",
		Dir:     "/usr/share/cups/doc-root",
		Options: []string{"bind", "ro"},
	})
	spec.AddMountEntry(osutil.MountEntry{
		Name:    "/var/lib/snapd/hostfs/usr/share/gimp/2.0/help",
		Dir:     "/usr/share/gimp/2.0/help",
		Options: []string{"bind", "ro"},
	})
	spec.AddMountEntry(osutil.MountEntry{
		Name:    "/var/lib/snapd/hostfs/usr/share/gtk-doc",
		Dir:     "/usr/share/gtk-doc",
		Options: []string{"bind", "ro"},
	})
	spec.AddMountEntry(osutil.MountEntry{
		Name:    "/var/lib/snapd/hostfs/usr/share/javascript/jquery",
		Dir:     "/usr/share/javascript/jquery",
		Options: []string{"bind", "ro"},
	})
	spec.AddMountEntry(osutil.MountEntry{
		Name:    "/var/lib/snapd/hostfs/usr/share/javascript/jquery-ui",
		Dir:     "/usr/share/javascript/jquery-ui",
		Options: []string{"bind", "ro"},
	})
	spec.AddMountEntry(osutil.MountEntry{
		Name:    "/var/lib/snapd/hostfs/usr/share/javascript/sphinxdoc",
		Dir:     "/usr/share/javascript/sphinxdoc",
		Options: []string{"bind", "ro"},
	})
	spec.AddMountEntry(osutil.MountEntry{
		Name:    "/var/lib/snapd/hostfs/usr/share/libreoffice/help",
		Dir:     "/usr/share/libreoffice/help",
		Options: []string{"bind", "ro"},
	})
	spec.AddMountEntry(osutil.MountEntry{
		Name:    "/var/lib/snapd/hostfs/usr/share/sphinx_rtd_theme",
		Dir:     "/usr/share/sphinx_rtd_theme",
		Options: []string{"bind", "ro"},
	})
	spec.AddMountEntry(osutil.MountEntry{
		Name:    "/var/lib/snapd/hostfs/usr/share/xubuntu-docs",
		Dir:     "/usr/share/xubuntu-docs",
		Options: []string{"bind", "ro"},
	})
	return nil
}

func init() {
	registerIface(&systemPackagesDocInterface{
		commonInterface: commonInterface{
			name:                 "system-packages-doc",
			summary:              systemPackagesDocSummary,
			implicitOnClassic:    true,
			baseDeclarationSlots: systemPackagesDocBaseDeclarationSlots,
			// affects the plug snap because of mount backend
			affectsPlugOnRefresh: true,
		},
	})
}
