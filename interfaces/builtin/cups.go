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
	"fmt"
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/mount"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
)

// On systems where the slot is provided by an app snap, the cups interface is
// the companion interface to the cups-control interface. The design of these
// interfaces is based on the idea that the slot implementation (eg cupsd) is
// expected to query snapd to determine if the cups-control interface is
// connected or not for the peer client process and the print service will
// mediate admin functionality (ie, the rules in these interfaces allow
// connecting to the print service, but do not implement enforcement rules; it
// is up to the print service to provide enforcement).
const cupsSummary = `allows access to the CUPS socket for printing`

// cups is currently only available via a providing app snap and this interface
// assumes that the providing app snap also slots 'cups-control' (the current
// design allows the snap provider to slots both cups-control and cups or just
// cups-control (like with implicit classic or any slot provider without
// mediation patches), but not just cups).
const cupsBaseDeclarationSlots = `
  cups:
    allow-installation:
      slot-snap-type:
        - app
    deny-connection: true
    deny-auto-connection: true
`

const cupsConnectedPlugAppArmor = `
# Allow communicating with the cups server

# Do not allow reading the user or global client.conf for this snap, as this may
# allow a user to point an application at an unconfined cupsd which could be
# used to load printer drivers etc. We only want client snaps with the cups
# interface plug connected to be able to talk to a version of cupsd which is
# strictly confined and performs mediation. This means only allowing to talk to
# /var/cups/cups.sock and not /run/cups/cups.sock since snapd has no way to know
# if the latter cupsd is confined and performs mediation, but the upstream
# maintained cups snap providing a cups slot will always perform mediation.
# As such, do not use the <abstractions/cups-client> include file here.

# Allow reading the personal settings for cups like default printer, etc.
owner @{HOME}/.cups/lpoptions r,

/{,var/}run/cups/printcap r,

# Allow talking to the snap version of cupsd socket that we expose via bind
# mounts from a snap providing the cups slot to this snap.
/var/cups/cups.sock rw,
`

type cupsInterface struct {
	commonInterface
}

func (iface *cupsInterface) AppArmorConnectedSlot(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	return nil
}

func validateCupsSocketDirSlotAttr(a interfaces.Attrer, snapInfo *snap.Info) (string, error) {
	// Allow an empty specification for the slot, in which case we don't perform
	// any mounts, etc. This is mainly to prevent errors in systems which still
	// have the old cups snap installed that haven't been updated to use the new
	// snap with the new slot declaration
	if _, ok := a.Lookup("cups-socket-directory"); !ok {
		return "", nil
	}

	var cupsdSocketSourceDir string
	if err := a.Attr("cups-socket-directory", &cupsdSocketSourceDir); err != nil {
		return "", err
	}

	// make sure that the cups socket dir is not an AppArmor Regular expression
	if err := apparmor.ValidateNoAppArmorRegexp(cupsdSocketSourceDir); err != nil {
		return "", fmt.Errorf("cups-socket-directory is not usable: %v", err)
	}

	if !cleanSubPath(cupsdSocketSourceDir) {
		return "", fmt.Errorf("cups-socket-directory is not clean: %q", cupsdSocketSourceDir)
	}

	// validate that the setting for cups-socket-directory is in $SNAP_DATA or
	// $SNAP_COMMON, we don't allow any other directories for the slot socket
	// dir
	// TODO: should we also allow /run/$SNAP_INSTANCE_NAME/ too ?
	if !strings.HasPrefix(cupsdSocketSourceDir, "$SNAP_COMMON") && !strings.HasPrefix(cupsdSocketSourceDir, "$SNAP_DATA") {
		return "", fmt.Errorf("cups-socket-directory must be a directory of $SNAP_COMMON or $SNAP_DATA")
	}
	// otherwise it must have a prefix of either SNAP_COMMON or SNAP_DATA,
	// validate that it has no other variables in it
	err := snap.ValidatePathVariables(cupsdSocketSourceDir)
	if err != nil {
		return "", err
	}

	// The path starts with $ and ValidatePathVariables() ensures
	// path contains only $SNAP, $SNAP_DATA, $SNAP_COMMON, and no
	// other $VARs are present. It is ok to use
	// ExpandSnapVariables() since it only expands $SNAP, $SNAP_DATA
	// and $SNAP_COMMON
	return snapInfo.ExpandSnapVariables(cupsdSocketSourceDir), nil
}

func (iface *cupsInterface) BeforePrepareSlot(slot *snap.SlotInfo) error {
	// verify that the snap has a cups-socket-directory interface attribute, which is
	// needed to identify where to find the cups socket is located in the snap
	// providing the cups socket
	_, err := validateCupsSocketDirSlotAttr(slot, slot.Snap)
	return err
}

func (iface *cupsInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	cupsdSocketSourceDir, err := validateCupsSocketDirSlotAttr(slot, slot.Snap())
	if err != nil {
		return err
	}

	// add the base snippet
	spec.AddSnippet(cupsConnectedPlugAppArmor)

	if cupsdSocketSourceDir == "" {
		// no other rules, this is the legacy slot without the additional
		// attribute
		return nil
	}

	// add rules to access the socket dir from the slot location directly
	// this is necessary otherwise clients get denials like this:
	// apparmor="DENIED" operation="connect"
	// profile="snap.test-snapd-cups-consumer.bin"
	// name="/var/snap/test-snapd-cups-provider/common/cups.sock"
	// pid=3195747 comm="nc" requested_mask="wr" denied_mask="wr" fsuid=0 ouid=0
	// this denial is the same that would happen for the content interface, so
	// we employ the same workaround from the content interface here too
	spec.AddSnippet(fmt.Sprintf(`
# In addition to the bind mount, add any AppArmor rules so that
# snaps may directly access the slot implementation's files. Due
# to a limitation in the kernel's LSM hooks for AF_UNIX, these
# are needed for using named sockets within the exported
# directory.
"%s/**" mrwklix,`, cupsdSocketSourceDir))

	// setup the snap-update-ns rules for bind mounting for the plugging snap
	emit := spec.AddUpdateNSf

	emit("  # Mount cupsd socket from cups snap to client snap\n")
	// note the trailing "/" is needed - we ensured that cupsdSocketSourceDir is
	// clean when we validated it, so it will not have a trailing "/" so we are
	// safe to add this here
	emit("  mount options=(rw bind) \"%s/\" -> /var/cups/,\n", cupsdSocketSourceDir)
	emit("  umount /var/cups/,\n")

	apparmor.GenWritableProfile(emit, cupsdSocketSourceDir, 1)
	apparmor.GenWritableProfile(emit, "/var/cups", 1)

	return nil
}

func (iface *cupsInterface) MountConnectedPlug(spec *mount.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	cupsdSocketSourceDir, err := validateCupsSocketDirSlotAttr(slot, slot.Snap())
	if err != nil {
		return err
	}

	if cupsdSocketSourceDir == "" {
		// no other rules, this is the legacy slot without the additional
		// attribute
		return nil
	}

	// add a bind mount of the cups-socket-directory to /var/cups of the plugging snap
	return spec.AddMountEntry(osutil.MountEntry{
		Name:    cupsdSocketSourceDir,
		Dir:     "/var/cups/",
		Options: []string{"bind", "rw"},
	})
}

func init() {
	registerIface(&cupsInterface{
		commonInterface: commonInterface{
			name:                 "cups",
			summary:              cupsSummary,
			implicitOnCore:       false,
			implicitOnClassic:    false,
			baseDeclarationSlots: cupsBaseDeclarationSlots,
		},
	})
}
