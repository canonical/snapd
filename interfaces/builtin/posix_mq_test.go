// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/seccomp"
	apparmor_sandbox "github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

const slotSnapInfoYaml = `name: producer
version: 1.0

slots:
  test-rw:
    interface: posix-mq
    path: /test-rw
    permissions:
      - read
      - write

  test-default:
    interface: posix-mq
    path: /test-default

  test-ro:
    interface: posix-mq
    path: /test-ro
    permissions:
      - read

  test-all-perms:
    interface: posix-mq
    path: /test-all-perms
    permissions:
      - create
      - delete
      - read
      - write

  test-path-array:
    interface: posix-mq
    path:
      - /test-array-1
      - /test-array-2
      - /test-array-3

  test-empty-path-array:
    interface: posix-mq
    path: []

  test-one-empty-path-array:
    interface: posix-mq
    path:
      - /test-array-1
      - ""

  test-empty-path:
    interface: posix-mq
    path: ""

  test-invalid-path-1:
    interface: posix-mq
    path: ../../test-invalid

  test-invalid-path-2:
    interface: posix-mq
    path: /test-invalid-2"[

  test-invalid-path-3:
    interface: posix-mq
    path:
      this-is-not-a-valid-path: true

  test-invalid-path-4:
    interface: posix-mq

  test-invalid-path-5:
    interface: posix-mq
    path: /.

  test-invalid-perms-1:
    interface: posix-mq
    path: /test-invalid-perms-1
    permissions:
      - create
      - delete
      - break-everything

  test-invalid-perms-2:
      interface: posix-mq
      path: /test-invalid-perms-2
      permissions: not-a-list

  test-invalid-perms-3:
    interface: posix-mq
    path: /test-invalid-perms-3
    permissions:
      - create
      - [not-a-string]

  test-label:
      interface: posix-mq
      posix-mq: this-is-a-test-label
      path: /test-label

  test-broken-label:
    interface: posix-mq
    posix-mq:
      - broken
    path: /test-default

apps:
  app:
    command: foo
    slots:
      - test-default-rw
      - test-rw
      - test-ro
      - test-all-perms
      - test-invalid-path-1
      - test-invalid-path-2
`

const defaultRWPlugSnapInfoYaml = `name: consumer
version: 1.0

plugs:
  test-default:
    interface: posix-mq

apps:
  app:
    command: foo
    plugs: [test-default]
`

const rwPlugSnapInfoYaml = `name: consumer
version: 1.0

plugs:
  test-rw:
    interface: posix-mq

apps:
  app:
    command: foo
    plugs: [test-rw]
`

const roPlugSnapInfoYaml = `name: consumer
version: 1.0

plugs:
  test-ro:
    interface: posix-mq

apps:
  app:
    command: foo
    plugs: [test-ro]
`

const allPermsPlugSnapInfoYaml = `name: consumer
version: 1.0

plugs:
  test-all-perms:
    interface: posix-mq

apps:
  app:
    command: foo
    plugs: [test-all-perms]
`

const pathArrayPlugSnapInfoYaml = `name: consumer
version: 1.0

plugs:
  test-path-array:
    interface: posix-mq

apps:
  app:
    command: foo
    plugs: [test-path-array]
`

const invalidPerms1PlugSnapInfoYaml = `name: consumer
version: 1.0

plugs:
  test-invalid-perms-1:
    interface: posix-mq

apps:
  app:
    command: foo
    plugs: [test-invalid-perms-1]
`

const testLabelPlugSnapInfoYaml = `name: consumer
version: 1.0

plugs:
  test-label:
    interface: posix-mq
    posix-mq: this-is-a-test-label

apps:
  app:
    command: foo
    plugs: [test-label]
`

const invalidPerms3PlugSnapInfoYaml = `name: consumer
version: 1.0

plugs:
  test-invalid-perms-3:
    interface: posix-mq

apps:
  app:
    command: foo
    plugs: [test-invalid-perms-3]
`

const testInvalidLabelPlugSnapInfoYaml = `name: consumer
version: 1.0

plugs:
  test-invalid-label:
    interface: posix-mq
    posix-mq:
      - this-is-a-broken-test-label

apps:
  app:
    command: foo
    plugs: [test-invalid-label]
`

type PosixMQInterfaceSuite struct {
	testutil.BaseTest

	iface interfaces.Interface

	testReadWriteSlotInfo *snap.SlotInfo
	testReadWriteSlot     *interfaces.ConnectedSlot
	testReadWritePlugInfo *snap.PlugInfo
	testReadWritePlug     *interfaces.ConnectedPlug

	testDefaultPermsSlotInfo *snap.SlotInfo
	testDefaultPermsSlot     *interfaces.ConnectedSlot
	testDefaultPermsPlugInfo *snap.PlugInfo
	testDefaultPermsPlug     *interfaces.ConnectedPlug

	testReadOnlySlotInfo *snap.SlotInfo
	testReadOnlySlot     *interfaces.ConnectedSlot
	testReadOnlyPlugInfo *snap.PlugInfo
	testReadOnlyPlug     *interfaces.ConnectedPlug

	testAllPermsSlotInfo *snap.SlotInfo
	testAllPermsSlot     *interfaces.ConnectedSlot
	testAllPermsPlugInfo *snap.PlugInfo
	testAllPermsPlug     *interfaces.ConnectedPlug

	testPathArraySlotInfo *snap.SlotInfo
	testPathArraySlot     *interfaces.ConnectedSlot
	testPathArrayPlugInfo *snap.PlugInfo
	testPathArrayPlug     *interfaces.ConnectedPlug

	testEmptyPathArraySlotInfo *snap.SlotInfo
	testEmptyPathArraySlot     *interfaces.ConnectedSlot

	testOneEmptyPathArraySlotInfo *snap.SlotInfo
	testOneEmptyPathArraySlot     *interfaces.ConnectedSlot

	testEmptyPathSlotInfo *snap.SlotInfo
	testEmptyPathSlot     *interfaces.ConnectedSlot

	testInvalidPath1SlotInfo *snap.SlotInfo
	testInvalidPath1Slot     *interfaces.ConnectedSlot

	testInvalidPath2SlotInfo *snap.SlotInfo
	testInvalidPath2Slot     *interfaces.ConnectedSlot

	testInvalidPath3SlotInfo *snap.SlotInfo
	testInvalidPath3Slot     *interfaces.ConnectedSlot

	testInvalidPath4SlotInfo *snap.SlotInfo
	testInvalidPath4Slot     *interfaces.ConnectedSlot

	testInvalidPath5SlotInfo *snap.SlotInfo
	testInvalidPath5Slot     *interfaces.ConnectedSlot

	testInvalidPerms1SlotInfo *snap.SlotInfo
	testInvalidPerms1Slot     *interfaces.ConnectedSlot
	testInvalidPerms1PlugInfo *snap.PlugInfo
	testInvalidPerms1Plug     *interfaces.ConnectedPlug

	testInvalidPerms2SlotInfo *snap.SlotInfo
	testInvalidPerms2Slot     *interfaces.ConnectedSlot

	testInvalidPerms3SlotInfo *snap.SlotInfo
	testInvalidPerms3Slot     *interfaces.ConnectedSlot
	testInvalidPerms3PlugInfo *snap.PlugInfo
	testInvalidPerms3Plug     *interfaces.ConnectedPlug

	testLabelSlotInfo *snap.SlotInfo
	testLabelSlot     *interfaces.ConnectedSlot
	testLabelPlugInfo *snap.PlugInfo
	testLabelPlug     *interfaces.ConnectedPlug

	testInvalidLabelSlotInfo *snap.SlotInfo
	testInvalidLabelSlot     *interfaces.ConnectedSlot
	testInvalidLabelPlugInfo *snap.PlugInfo
	testInvalidLabelPlug     *interfaces.ConnectedPlug
}

var _ = Suite(&PosixMQInterfaceSuite{
	iface: builtin.MustInterface("posix-mq"),
})

func (s *PosixMQInterfaceSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	slotSnap := snaptest.MockInfo(c, slotSnapInfoYaml, nil)

	s.testReadWriteSlotInfo = slotSnap.Slots["test-rw"]
	s.testReadWriteSlot = interfaces.NewConnectedSlot(s.testReadWriteSlotInfo, nil, nil)

	s.testDefaultPermsSlotInfo = slotSnap.Slots["test-default"]
	s.testDefaultPermsSlot = interfaces.NewConnectedSlot(s.testDefaultPermsSlotInfo, nil, nil)

	s.testReadOnlySlotInfo = slotSnap.Slots["test-ro"]
	s.testReadOnlySlot = interfaces.NewConnectedSlot(s.testReadOnlySlotInfo, nil, nil)

	s.testAllPermsSlotInfo = slotSnap.Slots["test-all-perms"]
	s.testAllPermsSlot = interfaces.NewConnectedSlot(s.testAllPermsSlotInfo, nil, nil)

	s.testPathArraySlotInfo = slotSnap.Slots["test-path-array"]
	s.testPathArraySlot = interfaces.NewConnectedSlot(s.testPathArraySlotInfo, nil, nil)

	s.testEmptyPathArraySlotInfo = slotSnap.Slots["test-empty-path-array"]
	s.testEmptyPathArraySlot = interfaces.NewConnectedSlot(s.testEmptyPathArraySlotInfo, nil, nil)

	s.testOneEmptyPathArraySlotInfo = slotSnap.Slots["test-one-empty-path-array"]
	s.testOneEmptyPathArraySlot = interfaces.NewConnectedSlot(s.testOneEmptyPathArraySlotInfo, nil, nil)

	s.testEmptyPathSlotInfo = slotSnap.Slots["test-empty-path"]
	s.testEmptyPathSlot = interfaces.NewConnectedSlot(s.testEmptyPathSlotInfo, nil, nil)

	s.testInvalidPath1SlotInfo = slotSnap.Slots["test-invalid-path-1"]
	s.testInvalidPath1Slot = interfaces.NewConnectedSlot(s.testInvalidPath1SlotInfo, nil, nil)

	s.testInvalidPath2SlotInfo = slotSnap.Slots["test-invalid-path-2"]
	s.testInvalidPath2Slot = interfaces.NewConnectedSlot(s.testInvalidPath2SlotInfo, nil, nil)

	s.testInvalidPath3SlotInfo = slotSnap.Slots["test-invalid-path-3"]
	s.testInvalidPath3Slot = interfaces.NewConnectedSlot(s.testInvalidPath3SlotInfo, nil, nil)

	s.testInvalidPath4SlotInfo = slotSnap.Slots["test-invalid-path-4"]
	s.testInvalidPath4Slot = interfaces.NewConnectedSlot(s.testInvalidPath4SlotInfo, nil, nil)

	s.testInvalidPath5SlotInfo = slotSnap.Slots["test-invalid-path-5"]
	s.testInvalidPath5Slot = interfaces.NewConnectedSlot(s.testInvalidPath5SlotInfo, nil, nil)

	s.testInvalidPerms1SlotInfo = slotSnap.Slots["test-invalid-perms-1"]
	s.testInvalidPerms1Slot = interfaces.NewConnectedSlot(s.testInvalidPerms1SlotInfo, nil, nil)

	s.testInvalidPerms2SlotInfo = slotSnap.Slots["test-invalid-perms-2"]
	s.testInvalidPerms2Slot = interfaces.NewConnectedSlot(s.testInvalidPerms2SlotInfo, nil, nil)

	s.testInvalidPerms3SlotInfo = slotSnap.Slots["test-invalid-perms-3"]
	s.testInvalidPerms3Slot = interfaces.NewConnectedSlot(s.testInvalidPerms3SlotInfo, nil, nil)

	s.testLabelSlotInfo = slotSnap.Slots["test-label"]
	s.testLabelSlot = interfaces.NewConnectedSlot(s.testLabelSlotInfo, nil, nil)

	s.testInvalidLabelSlotInfo = slotSnap.Slots["test-broken-label"]
	s.testInvalidLabelSlot = interfaces.NewConnectedSlot(s.testInvalidLabelSlotInfo, nil, nil)

	plugSnap0 := snaptest.MockInfo(c, rwPlugSnapInfoYaml, nil)
	s.testReadWritePlugInfo = plugSnap0.Plugs["test-rw"]
	s.testReadWritePlug = interfaces.NewConnectedPlug(s.testReadWritePlugInfo, nil, nil)

	plugSnap1 := snaptest.MockInfo(c, defaultRWPlugSnapInfoYaml, nil)
	s.testDefaultPermsPlugInfo = plugSnap1.Plugs["test-default"]
	s.testDefaultPermsPlug = interfaces.NewConnectedPlug(s.testDefaultPermsPlugInfo, nil, nil)

	plugSnap2 := snaptest.MockInfo(c, roPlugSnapInfoYaml, nil)
	s.testReadOnlyPlugInfo = plugSnap2.Plugs["test-ro"]
	s.testReadOnlyPlug = interfaces.NewConnectedPlug(s.testReadOnlyPlugInfo, nil, nil)

	plugSnap3 := snaptest.MockInfo(c, allPermsPlugSnapInfoYaml, nil)
	s.testAllPermsPlugInfo = plugSnap3.Plugs["test-all-perms"]
	s.testAllPermsPlug = interfaces.NewConnectedPlug(s.testAllPermsPlugInfo, nil, nil)

	plugSnap4 := snaptest.MockInfo(c, invalidPerms1PlugSnapInfoYaml, nil)
	s.testInvalidPerms1PlugInfo = plugSnap4.Plugs["test-invalid-perms-1"]
	s.testInvalidPerms1Plug = interfaces.NewConnectedPlug(s.testInvalidPerms1PlugInfo, nil, nil)

	plugSnap5 := snaptest.MockInfo(c, testLabelPlugSnapInfoYaml, nil)
	s.testLabelPlugInfo = plugSnap5.Plugs["test-label"]
	s.testLabelPlug = interfaces.NewConnectedPlug(s.testLabelPlugInfo, nil, nil)

	plugSnap6 := snaptest.MockInfo(c, testInvalidLabelPlugSnapInfoYaml, nil)
	s.testInvalidLabelPlugInfo = plugSnap6.Plugs["test-invalid-label"]
	s.testInvalidLabelPlug = interfaces.NewConnectedPlug(s.testInvalidLabelPlugInfo, nil, nil)

	plugSnap7 := snaptest.MockInfo(c, invalidPerms3PlugSnapInfoYaml, nil)
	s.testInvalidPerms3PlugInfo = plugSnap7.Plugs["test-invalid-perms-3"]
	s.testInvalidPerms3Plug = interfaces.NewConnectedPlug(s.testInvalidPerms3PlugInfo, nil, nil)

	plugSnap8 := snaptest.MockInfo(c, pathArrayPlugSnapInfoYaml, nil)
	s.testPathArrayPlugInfo = plugSnap8.Plugs["test-path-array"]
	s.testPathArrayPlug = interfaces.NewConnectedPlug(s.testPathArrayPlugInfo, nil, nil)
}

// splitSnippet converts the trimmed string snippet to a string slice
func splitSnippet(snippet string) []string {
	return strings.Split(strings.TrimSpace(snippet), "\n")
}

func (s *PosixMQInterfaceSuite) checkSlotSeccompSnippet(c *C, spec *seccomp.Specification) {
	slotSnippet := spec.SnippetForTag("snap.producer.app")

	c.Check(splitSnippet(slotSnippet), HasLen, 8)
	c.Check(slotSnippet, testutil.Contains, "mq_open")
	c.Check(slotSnippet, testutil.Contains, "mq_unlink")
	c.Check(slotSnippet, testutil.Contains, "mq_getsetattr")
	c.Check(slotSnippet, testutil.Contains, "mq_notify")
	c.Check(slotSnippet, testutil.Contains, "mq_timedreceive")
	c.Check(slotSnippet, testutil.Contains, "mq_timedreceive_time64")
	c.Check(slotSnippet, testutil.Contains, "mq_timedsend")
	c.Check(slotSnippet, testutil.Contains, "mq_timedsend_time64")
}

func (s *PosixMQInterfaceSuite) TestReadWriteMQAppArmor(c *C) {
	spec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.testReadWriteSlotInfo.Snap))
	err := spec.AddPermanentSlot(s.iface, s.testReadWriteSlotInfo)
	c.Assert(err, IsNil)

	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.producer.app"})

	slotSnippet := spec.SnippetForTag("snap.producer.app")
	c.Check(slotSnippet, testutil.Contains, `# POSIX Message Queue slot: test-rw`)
	c.Check(slotSnippet, testutil.Contains, `mqueue (open read write create delete) "/test-rw",`)

	spec = apparmor.NewSpecification(interfaces.NewSnapAppSet(s.testReadOnlyPlug.Snap()))
	err = spec.AddConnectedPlug(s.iface, s.testReadWritePlug, s.testReadWriteSlot)
	c.Assert(err, IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})

	plugSnippet := spec.SnippetForTag("snap.consumer.app")
	c.Check(plugSnippet, testutil.Contains, `# POSIX Message Queue plug: test-rw`)
	c.Check(plugSnippet, testutil.Contains, `mqueue (read write open) "/test-rw",`)
}

func (s *PosixMQInterfaceSuite) TestReadWriteMQSeccomp(c *C) {
	spec := seccomp.NewSpecification(interfaces.NewSnapAppSet(s.testReadWriteSlotInfo.Snap))
	err := spec.AddPermanentSlot(s.iface, s.testReadWriteSlotInfo)
	c.Assert(err, IsNil)

	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.producer.app"})

	s.checkSlotSeccompSnippet(c, spec)

	spec = seccomp.NewSpecification(interfaces.NewSnapAppSet(s.testReadWritePlug.Snap()))
	err = spec.AddConnectedPlug(s.iface, s.testReadWritePlug, s.testReadWriteSlot)
	c.Assert(err, IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})

	plugSnippet := spec.SnippetForTag("snap.consumer.app")
	c.Check(splitSnippet(plugSnippet), HasLen, 7)
	c.Check(plugSnippet, testutil.Contains, "mq_open")
	c.Check(plugSnippet, testutil.Contains, "mq_notify")
	c.Check(plugSnippet, testutil.Contains, "mq_timedreceive")
	c.Check(plugSnippet, testutil.Contains, "mq_timedreceive_time64")
	c.Check(plugSnippet, testutil.Contains, "mq_timedsend")
	c.Check(plugSnippet, testutil.Contains, "mq_timedsend_time64")
	c.Check(plugSnippet, testutil.Contains, "mq_getsetattr")
	c.Check(plugSnippet, Not(testutil.Contains), "mq_unlink")
}

func (s *PosixMQInterfaceSuite) TestDefaultReadWriteMQAppArmor(c *C) {
	spec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.testDefaultPermsSlotInfo.Snap))
	err := spec.AddPermanentSlot(s.iface, s.testDefaultPermsSlotInfo)
	c.Assert(err, IsNil)

	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.producer.app"})

	slotSnippet := spec.SnippetForTag("snap.producer.app")
	c.Check(slotSnippet, testutil.Contains, `# POSIX Message Queue slot: test-default`)
	c.Check(slotSnippet, testutil.Contains, `mqueue (open read write create delete) "/test-default",`)

	spec = apparmor.NewSpecification(interfaces.NewSnapAppSet(s.testDefaultPermsPlugInfo.Snap))
	err = spec.AddConnectedPlug(s.iface, s.testDefaultPermsPlug, s.testDefaultPermsSlot)
	c.Assert(err, IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})

	plugSnippet := spec.SnippetForTag("snap.consumer.app")
	c.Check(plugSnippet, testutil.Contains, `# POSIX Message Queue plug: test-default`)
	c.Check(plugSnippet, testutil.Contains, `mqueue (read write open) "/test-default",`)
}

func (s *PosixMQInterfaceSuite) TestDefaultReadWriteMQSeccomp(c *C) {
	spec := seccomp.NewSpecification(interfaces.NewSnapAppSet(s.testDefaultPermsSlotInfo.Snap))
	err := spec.AddPermanentSlot(s.iface, s.testDefaultPermsSlotInfo)
	c.Assert(err, IsNil)

	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.producer.app"})
	s.checkSlotSeccompSnippet(c, spec)

	spec = seccomp.NewSpecification(interfaces.NewSnapAppSet(s.testDefaultPermsPlugInfo.Snap))
	err = spec.AddConnectedPlug(s.iface, s.testDefaultPermsPlug, s.testDefaultPermsSlot)
	c.Assert(err, IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})

	plugSnippet := spec.SnippetForTag("snap.consumer.app")
	c.Check(splitSnippet(plugSnippet), HasLen, 7)
	c.Check(plugSnippet, testutil.Contains, "mq_open")
	c.Check(plugSnippet, testutil.Contains, "mq_notify")
	c.Check(plugSnippet, testutil.Contains, "mq_timedreceive")
	c.Check(plugSnippet, testutil.Contains, "mq_timedreceive_time64")
	c.Check(plugSnippet, testutil.Contains, "mq_timedsend")
	c.Check(plugSnippet, testutil.Contains, "mq_timedsend_time64")
	c.Check(plugSnippet, testutil.Contains, "mq_getsetattr")
	c.Check(plugSnippet, Not(testutil.Contains), "mq_unlink")
}

func (s *PosixMQInterfaceSuite) TestReadOnlyMQAppArmor(c *C) {
	spec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.testReadOnlySlotInfo.Snap))
	err := spec.AddPermanentSlot(s.iface, s.testReadOnlySlotInfo)
	c.Assert(err, IsNil)

	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.producer.app"})

	slotSnippet := spec.SnippetForTag("snap.producer.app")
	c.Check(slotSnippet, testutil.Contains, `mqueue (open read write create delete) "/test-ro",`)

	spec = apparmor.NewSpecification(interfaces.NewSnapAppSet(s.testReadOnlyPlug.Snap()))
	err = spec.AddConnectedPlug(s.iface, s.testReadOnlyPlug, s.testReadOnlySlot)
	c.Assert(err, IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})

	plugSnippet := spec.SnippetForTag("snap.consumer.app")
	c.Check(plugSnippet, testutil.Contains, `mqueue (read open) "/test-ro",`)
}

func (s *PosixMQInterfaceSuite) TestReadOnlyMQSeccomp(c *C) {
	spec := seccomp.NewSpecification(interfaces.NewSnapAppSet(s.testReadOnlySlotInfo.Snap))
	err := spec.AddPermanentSlot(s.iface, s.testReadOnlySlotInfo)
	c.Assert(err, IsNil)

	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.producer.app"})

	s.checkSlotSeccompSnippet(c, spec)

	spec = seccomp.NewSpecification(interfaces.NewSnapAppSet(s.testReadOnlyPlug.Snap()))
	err = spec.AddConnectedPlug(s.iface, s.testReadOnlyPlug, s.testReadOnlySlot)
	c.Assert(err, IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})

	plugSnippet := spec.SnippetForTag("snap.consumer.app")
	c.Check(splitSnippet(plugSnippet), HasLen, 5)
	c.Check(plugSnippet, testutil.Contains, "mq_open")
	c.Check(plugSnippet, testutil.Contains, "mq_notify")
	c.Check(plugSnippet, testutil.Contains, "mq_timedreceive")
	c.Check(plugSnippet, testutil.Contains, "mq_timedreceive_time64")
	c.Check(plugSnippet, testutil.Contains, "mq_getsetattr")
	c.Check(plugSnippet, Not(testutil.Contains), "mq_timedsend")
	c.Check(plugSnippet, Not(testutil.Contains), "mq_timedsend_time64")
	c.Check(plugSnippet, Not(testutil.Contains), "mq_unlink")
}

func (s *PosixMQInterfaceSuite) TestPathArrayMQAppArmor(c *C) {
	spec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.testPathArraySlotInfo.Snap))
	err := spec.AddPermanentSlot(s.iface, s.testPathArraySlotInfo)
	c.Assert(err, IsNil)

	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.producer.app"})

	slotSnippet := spec.SnippetForTag("snap.producer.app")
	c.Check(slotSnippet, testutil.Contains, `  mqueue (open read write create delete) "/test-array-1",
  mqueue (open read write create delete) "/test-array-2",
  mqueue (open read write create delete) "/test-array-3",
`)

	spec = apparmor.NewSpecification(interfaces.NewSnapAppSet(s.testPathArrayPlugInfo.Snap))
	err = spec.AddConnectedPlug(s.iface, s.testPathArrayPlug, s.testPathArraySlot)
	c.Assert(err, IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})

	plugSnippet := spec.SnippetForTag("snap.consumer.app")
	c.Check(plugSnippet, testutil.Contains, `  mqueue (read write open) "/test-array-1",
  mqueue (read write open) "/test-array-2",
  mqueue (read write open) "/test-array-3",
`)
}

func (s *PosixMQInterfaceSuite) TestPathArrayMQSeccomp(c *C) {
	spec := seccomp.NewSpecification(interfaces.NewSnapAppSet(s.testPathArraySlotInfo.Snap))
	err := spec.AddPermanentSlot(s.iface, s.testPathArraySlotInfo)
	c.Assert(err, IsNil)

	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.producer.app"})

	s.checkSlotSeccompSnippet(c, spec)

	spec = seccomp.NewSpecification(interfaces.NewSnapAppSet(s.testPathArrayPlug.Snap()))
	err = spec.AddConnectedPlug(s.iface, s.testPathArrayPlug, s.testPathArraySlot)
	c.Assert(err, IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})

	plugSnippet := spec.SnippetForTag("snap.consumer.app")
	c.Check(splitSnippet(plugSnippet), HasLen, 7)
	c.Check(plugSnippet, testutil.Contains, "mq_open")
	c.Check(plugSnippet, testutil.Contains, "mq_notify")
	c.Check(plugSnippet, testutil.Contains, "mq_timedreceive")
	c.Check(plugSnippet, testutil.Contains, "mq_timedreceive_time64")
	c.Check(plugSnippet, testutil.Contains, "mq_timedsend")
	c.Check(plugSnippet, testutil.Contains, "mq_timedsend_time64")
	c.Check(plugSnippet, testutil.Contains, "mq_getsetattr")
	c.Check(plugSnippet, Not(testutil.Contains), "mq_unlink")
}

func (s *PosixMQInterfaceSuite) TestAllPermsMQAppArmor(c *C) {
	spec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.testAllPermsSlotInfo.Snap))
	err := spec.AddPermanentSlot(s.iface, s.testAllPermsSlotInfo)
	c.Assert(err, IsNil)

	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.producer.app"})

	slotSnippet := spec.SnippetForTag("snap.producer.app")
	c.Check(slotSnippet, testutil.Contains, `mqueue (open read write create delete) "/test-all-perms",`)

	spec = apparmor.NewSpecification(interfaces.NewSnapAppSet(s.testAllPermsPlugInfo.Snap))
	err = spec.AddConnectedPlug(s.iface, s.testAllPermsPlug, s.testAllPermsSlot)
	c.Assert(err, IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})

	plugSnippet := spec.SnippetForTag("snap.consumer.app")
	c.Check(plugSnippet, testutil.Contains, `mqueue (create delete read write open) "/test-all-perms",`)
}

func (s *PosixMQInterfaceSuite) TestAllPermsMQSeccomp(c *C) {
	spec := seccomp.NewSpecification(interfaces.NewSnapAppSet(s.testAllPermsSlotInfo.Snap))
	err := spec.AddPermanentSlot(s.iface, s.testAllPermsSlotInfo)
	c.Assert(err, IsNil)

	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.producer.app"})
	s.checkSlotSeccompSnippet(c, spec)

	spec = seccomp.NewSpecification(interfaces.NewSnapAppSet(s.testAllPermsPlugInfo.Snap))
	err = spec.AddConnectedPlug(s.iface, s.testAllPermsPlug, s.testAllPermsSlot)
	c.Assert(err, IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})

	plugSnippet := spec.SnippetForTag("snap.consumer.app")
	c.Check(splitSnippet(plugSnippet), HasLen, 8)
	c.Check(plugSnippet, testutil.Contains, "mq_open")
	c.Check(plugSnippet, testutil.Contains, "mq_unlink")
	c.Check(plugSnippet, testutil.Contains, "mq_getsetattr")
	c.Check(plugSnippet, testutil.Contains, "mq_notify")
	c.Check(plugSnippet, testutil.Contains, "mq_timedreceive")
	c.Check(plugSnippet, testutil.Contains, "mq_timedreceive_time64")
	c.Check(plugSnippet, testutil.Contains, "mq_timedsend")
	c.Check(plugSnippet, testutil.Contains, "mq_timedsend_time64")
}

func (s *PosixMQInterfaceSuite) TestPathValidationPosixMQ(c *C) {
	spec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.testInvalidPath1SlotInfo.Snap))
	err := spec.AddPermanentSlot(s.iface, s.testInvalidPath1SlotInfo)
	c.Check(err, ErrorMatches,
		`posix-mq "path" attribute must conform to the POSIX message queue name specifications \(see "man mq_overview"\): /../../test-invalid`)
}

func (s *PosixMQInterfaceSuite) TestPathValidationAppArmorRegex(c *C) {
	spec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.testInvalidPath2SlotInfo.Snap))
	err := spec.AddPermanentSlot(s.iface, s.testInvalidPath2SlotInfo)
	c.Check(err, ErrorMatches, `posix-mq "path" attribute is invalid: /test-invalid-2"\["`)
}

func (s *PosixMQInterfaceSuite) TestPathStringValidation(c *C) {
	spec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.testInvalidPath3SlotInfo.Snap))
	err := spec.AddPermanentSlot(s.iface, s.testInvalidPath3SlotInfo)
	c.Check(err, ErrorMatches, `snap "producer" has interface "posix-mq" with invalid value type map\[string\]interface {} for "path" attribute: \*\[\]string`)
}

func (s *PosixMQInterfaceSuite) TestInvalidPerms1(c *C) {
	spec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.testInvalidPerms1SlotInfo.Snap))
	// The slot should function correctly here as it receives the full list
	// of built-in permissions, not what's listed in the configuration
	err := spec.AddPermanentSlot(s.iface, s.testInvalidPerms1SlotInfo)
	c.Assert(err, IsNil)

	spec = apparmor.NewSpecification(interfaces.NewSnapAppSet(s.testInvalidPerms1PlugInfo.Snap))
	// The plug should fail to connect as it receives the given list of
	// invalid permissions
	err = spec.AddConnectedPlug(s.iface, s.testInvalidPerms1Plug, s.testInvalidPerms1Slot)
	c.Check(err, ErrorMatches,
		`posix-mq slot permission "break-everything" not valid, must be one of \[open read write create delete\]`)
}

func (s *PosixMQInterfaceSuite) TestInvalidPerms3(c *C) {
	spec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.testInvalidPerms3PlugInfo.Snap))
	err := spec.AddConnectedPlug(s.iface, s.testInvalidPerms3Plug, s.testInvalidPerms3Slot)
	c.Check(err, ErrorMatches,
		`snap "producer" has interface "posix-mq" with invalid value type \[\]interface {} for "permissions" attribute: \*\[\]string`)
}

func (s *PosixMQInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "posix-mq")
}

func (s *PosixMQInterfaceSuite) TestNoAppArmor(c *C) {
	// Ensure that the interface does not fail if AppArmor is unsupported
	restore := apparmor_sandbox.MockLevel(apparmor_sandbox.Unsupported)
	defer restore()

	c.Check(interfaces.BeforePrepareSlot(s.iface, s.testReadWriteSlotInfo), IsNil)
	c.Check(interfaces.BeforePreparePlug(s.iface, s.testReadWritePlugInfo), IsNil)
}

func (s *PosixMQInterfaceSuite) TestFeatureDetection(c *C) {
	// Ensure that the interface fails if the mqueue feature is not present
	restore := apparmor_sandbox.MockFeatures(nil, nil, nil, nil)
	defer restore()
	c.Check(interfaces.BeforePrepareSlot(s.iface, s.testReadWriteSlotInfo), ErrorMatches,
		`AppArmor does not support POSIX message queues - cannot setup or connect interfaces`)
}

func (s *PosixMQInterfaceSuite) checkSlotPosixMQAttr(c *C, slot *snap.SlotInfo) {
	c.Check(slot.Attrs["posix-mq"], Equals, slot.Name)
}

func (s *PosixMQInterfaceSuite) checkPlugPosixMQAttr(c *C, plug *snap.PlugInfo) {
	c.Check(plug.Attrs["posix-mq"], Equals, plug.Name)
}

func (s *PosixMQInterfaceSuite) TestSanitizeSlot(c *C) {
	// Ensure that the mqueue feature is detected
	restore := apparmor_sandbox.MockFeatures([]string{}, nil, []string{"mqueue"}, nil)
	defer restore()

	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.testReadWriteSlotInfo), IsNil)
	s.checkSlotPosixMQAttr(c, s.testReadWriteSlotInfo)
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.testDefaultPermsSlotInfo), IsNil)
	s.checkSlotPosixMQAttr(c, s.testDefaultPermsSlotInfo)
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.testReadOnlySlotInfo), IsNil)
	s.checkSlotPosixMQAttr(c, s.testReadOnlySlotInfo)
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.testAllPermsSlotInfo), IsNil)
	s.checkSlotPosixMQAttr(c, s.testAllPermsSlotInfo)
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.testPathArraySlotInfo), IsNil)
	s.checkSlotPosixMQAttr(c, s.testPathArraySlotInfo)
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.testLabelSlotInfo), IsNil)
	c.Check(s.testLabelSlotInfo.Attrs["posix-mq"], Equals, "this-is-a-test-label")

	// These should return errors due to invalid configuration
	c.Check(interfaces.BeforePrepareSlot(s.iface, s.testInvalidPath1SlotInfo), ErrorMatches,
		`posix-mq "path" attribute must conform to the POSIX message queue name specifications \(see "man mq_overview"\): /../../test-invalid`)
	c.Check(interfaces.BeforePrepareSlot(s.iface, s.testInvalidPath2SlotInfo), ErrorMatches,
		`posix-mq "path" attribute is invalid: /test-invalid-2"\["`)
	c.Check(interfaces.BeforePrepareSlot(s.iface, s.testInvalidPath3SlotInfo), ErrorMatches,
		`snap "producer" has interface "posix-mq" with invalid value type map\[string\]interface {} for "path" attribute: \*\[\]string`)
	c.Check(interfaces.BeforePrepareSlot(s.iface, s.testInvalidPath4SlotInfo), ErrorMatches,
		`posix-mq slot requires the "path" attribute`)
	c.Check(interfaces.BeforePrepareSlot(s.iface, s.testInvalidPath5SlotInfo), ErrorMatches,
		`posix-mq "path" attribute is not a clean path: "/."`)
	c.Check(interfaces.BeforePrepareSlot(s.iface, s.testInvalidPerms1SlotInfo), ErrorMatches,
		`posix-mq slot permission "break-everything" not valid, must be one of \[open read write create delete\]`)
	c.Check(interfaces.BeforePrepareSlot(s.iface, s.testInvalidPerms2SlotInfo), ErrorMatches,
		`snap "producer" has interface "posix-mq" with invalid value type string for "permissions" attribute: \*\[\]string`)
	c.Check(interfaces.BeforePrepareSlot(s.iface, s.testInvalidLabelSlotInfo), ErrorMatches,
		`posix-mq "posix-mq" attribute must be a string, not \[broken\]`)
	c.Check(interfaces.BeforePrepareSlot(s.iface, s.testEmptyPathArraySlotInfo), ErrorMatches,
		`posix-mq slot requires at least one value in the "path" attribute`)
	c.Check(interfaces.BeforePrepareSlot(s.iface, s.testEmptyPathSlotInfo), ErrorMatches,
		`posix-mq slot "path" attribute values cannot be empty`)
	c.Check(interfaces.BeforePrepareSlot(s.iface, s.testOneEmptyPathArraySlotInfo), ErrorMatches,
		`posix-mq slot "path" attribute values cannot be empty`)
}

func (s *PosixMQInterfaceSuite) TestSanitizePlug(c *C) {
	// Ensure that the mqueue feature is detected
	restore := apparmor_sandbox.MockFeatures([]string{}, nil, []string{"mqueue"}, nil)
	defer restore()

	c.Assert(interfaces.BeforePreparePlug(s.iface, s.testReadWritePlugInfo), IsNil)
	s.checkPlugPosixMQAttr(c, s.testReadWritePlugInfo)
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.testDefaultPermsPlugInfo), IsNil)
	s.checkPlugPosixMQAttr(c, s.testDefaultPermsPlugInfo)
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.testReadOnlyPlugInfo), IsNil)
	s.checkPlugPosixMQAttr(c, s.testReadOnlyPlugInfo)
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.testAllPermsPlugInfo), IsNil)
	s.checkPlugPosixMQAttr(c, s.testAllPermsPlugInfo)
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.testPathArrayPlugInfo), IsNil)
	s.checkPlugPosixMQAttr(c, s.testPathArrayPlugInfo)
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.testInvalidPerms1PlugInfo), IsNil)
	s.checkPlugPosixMQAttr(c, s.testInvalidPerms1PlugInfo)
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.testLabelPlugInfo), IsNil)
	c.Check(s.testLabelPlugInfo.Attrs["posix-mq"], Equals, "this-is-a-test-label")

	c.Check(interfaces.BeforePreparePlug(s.iface, s.testInvalidLabelPlugInfo), ErrorMatches,
		`posix-mq "posix-mq" attribute must be a string, not \[this-is-a-broken-test-label\]`)
}

func (s *PosixMQInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}

func (s *PosixMQInterfaceSuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(s.testReadWritePlugInfo, s.testReadWriteSlotInfo), Equals, true)
}

func (s *PosixMQInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Check(si.ImplicitOnCore, Equals, false)
	c.Check(si.ImplicitOnClassic, Equals, false)
	c.Check(si.Summary, Equals, `allows access to POSIX message queues`)
}
