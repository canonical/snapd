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

package builtin_test

import (
	"fmt"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/mount"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type cupsSuite struct {
	iface interfaces.Interface

	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug

	providerSlotInfo *snap.SlotInfo
	providerSlot     *interfaces.ConnectedSlot

	providerLegacySlotInfo *snap.SlotInfo
	providerLegacySlot     *interfaces.ConnectedSlot
}

var _ = Suite(&cupsSuite{iface: builtin.MustInterface("cups")})

const cupsConsumerYaml = `name: consumer
version: 0
apps:
 app:
  plugs: [cups]
`

const cupsProviderYaml = `name: provider
version: 0
slots:
  cups-socket:
    interface: cups
    cups-socket-directory: $SNAP_COMMON/foo-subdir
apps:
 app:
  slots: [cups-socket]
`

const cupsProviderLegacyYaml = `name: provider
version: 0
slots:
  # no attribute
  cups: {}
`

func (s *cupsSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, cupsConsumerYaml, nil, "cups")
	s.providerSlot, s.providerSlotInfo = MockConnectedSlot(c, cupsProviderYaml, nil, "cups-socket")
	s.providerLegacySlot, s.providerLegacySlotInfo = MockConnectedSlot(c, cupsProviderLegacyYaml, nil, "cups")
}

func (s *cupsSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "cups")
}

func (s *cupsSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.providerSlotInfo), IsNil)
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.providerLegacySlotInfo), IsNil)
}

const invalidCupsProviderYamlFmt = `name: provider
version: 0
slots:
  cups-socket:
    interface: cups
    # regex not allowed
    cups-socket-directory: %s
apps:
 app:
  slots: [cups-socket]
`

func (s *cupsSuite) TestSanitizeInvalidSlot(c *C) {
	tt := []struct {
		snippet string
		err     string
	}{
		{
			"$SNAP_COMMON/foo-subdir/*",
			"cups-socket-directory is not usable: .* contains a reserved apparmor char .*",
		},
		{
			"$SNAP_COMMON/foo-subdir/../../../",
			`cups-socket-directory is not clean: \"\$SNAP_COMMON/foo-subdir/../../../\"`,
		},
		{
			"$SNAP/foo",
			`cups-socket-directory must be a directory of \$SNAP_COMMON or \$SNAP_DATA`,
		},
	}

	for _, t := range tt {
		yaml := fmt.Sprintf(invalidCupsProviderYamlFmt, t.snippet)

		_, invalidSlotInfo := MockConnectedSlot(c, yaml, nil, "cups-socket")
		mylog.Check(interfaces.BeforePrepareSlot(s.iface, invalidSlotInfo))
		c.Assert(err, ErrorMatches, t.err)
	}
}

func (s *cupsSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

const expSnapUpdateNsSnippet = `  # Mount cupsd socket from cups snap to client snap
  mount options=(rw bind) "/var/snap/provider/common/foo-subdir/" -> /var/cups/,
  umount /var/cups/,
  # Writable directory /var/snap/provider/common/foo-subdir
  "/var/snap/provider/common/foo-subdir/" rw,
  "/var/snap/provider/common/" rw,
  "/var/snap/provider/" rw,
  # Writable mimic /var
  # .. permissions for traversing the prefix that is assumed to exist
  # .. variant with mimic at /
  # Allow reading the mimic directory, it must exist in the first place.
  "/" r,
  # Allow setting the read-only directory aside via a bind mount.
  "/tmp/.snap/" rw,
  mount options=(rbind, rw) "/" -> "/tmp/.snap/",
  # Allow mounting tmpfs over the read-only directory.
  mount fstype=tmpfs options=(rw) tmpfs -> "/",
  # Allow creating empty files and directories for bind mounting things
  # to reconstruct the now-writable parent directory.
  "/tmp/.snap/*/" rw,
  "/*/" rw,
  mount options=(rbind, rw) "/tmp/.snap/*/" -> "/*/",
  "/tmp/.snap/*" rw,
  "/*" rw,
  mount options=(bind, rw) "/tmp/.snap/*" -> "/*",
  # Allow unmounting the auxiliary directory.
  # TODO: use fstype=tmpfs here for more strictness (LP: #1613403)
  mount options=(rprivate) -> "/tmp/.snap/",
  umount "/tmp/.snap/",
  # Allow unmounting the destination directory as well as anything
  # inside.  This lets us perform the undo plan in case the writable
  # mimic fails.
  mount options=(rprivate) -> "/",
  mount options=(rprivate) -> "/*",
  mount options=(rprivate) -> "/*/",
  umount "/",
  umount "/*",
  umount "/*/",
  # .. variant with mimic at /var/
  "/var/" r,
  "/tmp/.snap/var/" rw,
  mount options=(rbind, rw) "/var/" -> "/tmp/.snap/var/",
  mount fstype=tmpfs options=(rw) tmpfs -> "/var/",
  "/tmp/.snap/var/*/" rw,
  "/var/*/" rw,
  mount options=(rbind, rw) "/tmp/.snap/var/*/" -> "/var/*/",
  "/tmp/.snap/var/*" rw,
  "/var/*" rw,
  mount options=(bind, rw) "/tmp/.snap/var/*" -> "/var/*",
  mount options=(rprivate) -> "/tmp/.snap/var/",
  umount "/tmp/.snap/var/",
  mount options=(rprivate) -> "/var/",
  mount options=(rprivate) -> "/var/*",
  mount options=(rprivate) -> "/var/*/",
  umount "/var/",
  umount "/var/*",
  umount "/var/*/",
`

func (s *cupsSuite) TestAppArmorSpec(c *C) {
	// consumer to provider on core for ConnectedPlug
	appSet := mylog.Check2(interfaces.NewSnapAppSet(s.plug.Snap(), nil))

	spec := apparmor.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.providerSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "# Allow communicating with the cups server")
	// no cups abstractions
	c.Assert(spec.SnippetForTag("snap.consumer.app"), Not(testutil.Contains), "#include <abstractions/cups-client>")
	// but has the lpoptions config file though
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `owner @{HOME}/.cups/lpoptions r,`)
	// the special mount rules are present
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `"/var/snap/provider/common/foo-subdir/**" mrwklix,`)
	// the writable mimic profile for snap-update-ns is generated as well
	c.Assert(strings.Join(spec.UpdateNS(), ""), Equals, expSnapUpdateNsSnippet)

	// consumer to legacy provider
	appSet = mylog.Check2(interfaces.NewSnapAppSet(s.plug.Snap(), nil))

	specLegacy := apparmor.NewSpecification(appSet)
	c.Assert(specLegacy.AddConnectedPlug(s.iface, s.plug, s.providerLegacySlot), IsNil)
	c.Assert(specLegacy.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(specLegacy.SnippetForTag("snap.consumer.app"), testutil.Contains, "# Allow communicating with the cups server")
	// no cups abstractions
	c.Assert(specLegacy.SnippetForTag("snap.consumer.app"), Not(testutil.Contains), "#include <abstractions/cups-client>")
	// but has the lpoptions config file though
	c.Assert(specLegacy.SnippetForTag("snap.consumer.app"), testutil.Contains, `owner @{HOME}/.cups/lpoptions r,`)
	// no special mounting rules
	c.Assert(specLegacy.SnippetForTag("snap.consumer.app"), Not(testutil.Contains), "/var/snap/provider/common/foo-subdir/** mrwklix,")
	// no writable mimic profile for snap-update-ns
	c.Assert(specLegacy.UpdateNS(), HasLen, 0)
}

func (s *cupsSuite) TestMountSpec(c *C) {
	// consumer to provider on core for ConnectedPlug
	spec := &mount.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.providerSlot), IsNil)
	// mount entry for /var/cups/ for all namespaces
	c.Assert(spec.MountEntries(), DeepEquals, []osutil.MountEntry{
		{
			Name:    "/var/snap/provider/common/foo-subdir",
			Dir:     "/var/cups/",
			Options: []string{"bind", "rw"},
		},
	})
	// no user specific mounts
	c.Assert(spec.UserMountEntries(), HasLen, 0)

	// consumer to legacy provider has no mounts at all
	specLegacy := &mount.Specification{}
	c.Assert(specLegacy.AddConnectedPlug(s.iface, s.plug, s.providerLegacySlot), IsNil)
	c.Assert(specLegacy.MountEntries(), HasLen, 0)
	c.Assert(specLegacy.UserMountEntries(), HasLen, 0)
}

func (s *cupsSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, false)
	c.Assert(si.ImplicitOnClassic, Equals, false)
	c.Assert(si.Summary, Equals, `allows access to the CUPS socket for printing`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "cups")
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "deny-connection: true")
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "deny-auto-connection: true")
}

func (s *cupsSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
