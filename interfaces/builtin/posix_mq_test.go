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

  test-invalid-path-1:
    interface: posix-mq
    path: ../../test-invalid

  test-invalid-path-2:
    interface: posix-mq
    path: /test-invalid-2"[

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

type PosixMQInterfaceSuite struct {
	testutil.BaseTest

	iface interfaces.Interface

	testSlotInfo0 *snap.SlotInfo
	testSlot0     *interfaces.ConnectedSlot
	testPlugInfo0 *snap.PlugInfo
	testPlug0     *interfaces.ConnectedPlug

	testSlotInfo1 *snap.SlotInfo
	testSlot1     *interfaces.ConnectedSlot
	testPlugInfo1 *snap.PlugInfo
	testPlug1     *interfaces.ConnectedPlug

	testSlotInfo2 *snap.SlotInfo
	testSlot2     *interfaces.ConnectedSlot
	testPlugInfo2 *snap.PlugInfo
	testPlug2     *interfaces.ConnectedPlug

	testSlotInfo3 *snap.SlotInfo
	testSlot3     *interfaces.ConnectedSlot
	testPlugInfo3 *snap.PlugInfo
	testPlug3     *interfaces.ConnectedPlug

	testSlotInfo4 *snap.SlotInfo
	testSlot4     *interfaces.ConnectedSlot

	testSlotInfo5 *snap.SlotInfo
	testSlot5     *interfaces.ConnectedSlot
}

var _ = Suite(&PosixMQInterfaceSuite{
	iface: builtin.MustInterface("posix-mq"),
})

func (s *PosixMQInterfaceSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	slotSnap := snaptest.MockInfo(c, slotSnapInfoYaml, nil)

	s.testSlotInfo0 = slotSnap.Slots["test-rw"]
	s.testSlot0 = interfaces.NewConnectedSlot(s.testSlotInfo0, nil, nil)

	s.testSlotInfo1 = slotSnap.Slots["test-default"]
	s.testSlot1 = interfaces.NewConnectedSlot(s.testSlotInfo1, nil, nil)

	s.testSlotInfo2 = slotSnap.Slots["test-ro"]
	s.testSlot2 = interfaces.NewConnectedSlot(s.testSlotInfo2, nil, nil)

	s.testSlotInfo3 = slotSnap.Slots["test-all-perms"]
	s.testSlot3 = interfaces.NewConnectedSlot(s.testSlotInfo3, nil, nil)

	s.testSlotInfo4 = slotSnap.Slots["test-invalid-path-1"]
	s.testSlot4 = interfaces.NewConnectedSlot(s.testSlotInfo4, nil, nil)

	s.testSlotInfo5 = slotSnap.Slots["test-invalid-path-2"]
	s.testSlot5 = interfaces.NewConnectedSlot(s.testSlotInfo5, nil, nil)

	plugSnap0 := snaptest.MockInfo(c, rwPlugSnapInfoYaml, nil)
	s.testPlugInfo0 = plugSnap0.Plugs["test-rw"]
	s.testPlug0 = interfaces.NewConnectedPlug(s.testPlugInfo0, nil, nil)

	plugSnap1 := snaptest.MockInfo(c, defaultRWPlugSnapInfoYaml, nil)
	s.testPlugInfo1 = plugSnap1.Plugs["test-default"]
	s.testPlug1 = interfaces.NewConnectedPlug(s.testPlugInfo1, nil, nil)

	plugSnap2 := snaptest.MockInfo(c, roPlugSnapInfoYaml, nil)
	s.testPlugInfo2 = plugSnap2.Plugs["test-ro"]
	s.testPlug2 = interfaces.NewConnectedPlug(s.testPlugInfo2, nil, nil)

	plugSnap3 := snaptest.MockInfo(c, allPermsPlugSnapInfoYaml, nil)
	s.testPlugInfo3 = plugSnap3.Plugs["test-all-perms"]
	s.testPlug3 = interfaces.NewConnectedPlug(s.testPlugInfo3, nil, nil)
}

func (s *PosixMQInterfaceSuite) checkSlotSeccompSnippet(c *C, spec *seccomp.Specification) {
	slotSnippet := spec.SnippetForTag("snap.producer.app")
	c.Check(slotSnippet, testutil.Contains, "mq_open")
	c.Check(slotSnippet, testutil.Contains, "mq_unlink")
	c.Check(slotSnippet, testutil.Contains, "mq_getsetattr")
	c.Check(slotSnippet, testutil.Contains, "mq_notify")
	c.Check(slotSnippet, testutil.Contains, "mq_timedreceive")
	c.Check(slotSnippet, testutil.Contains, "mq_timedsend")
}

func (s *PosixMQInterfaceSuite) TestReadWriteMQAppArmor(c *C) {
	spec := &apparmor.Specification{}
	err := spec.AddPermanentSlot(s.iface, s.testSlotInfo0)
	c.Assert(err, IsNil)
	err = spec.AddConnectedPlug(s.iface, s.testPlug0, s.testSlot0)
	c.Assert(err, IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app", "snap.producer.app"})

	slotSnippet := spec.SnippetForTag("snap.producer.app")
	c.Check(slotSnippet, testutil.Contains, `mqueue (open read write create delete) "/test-rw",`)

	plugSnippet := spec.SnippetForTag("snap.consumer.app")
	c.Check(plugSnippet, testutil.Contains, `mqueue (read write open) "/test-rw",`)
}

func (s *PosixMQInterfaceSuite) TestReadWriteMQSeccomp(c *C) {
	spec := &seccomp.Specification{}
	err := spec.AddPermanentSlot(s.iface, s.testSlotInfo0)
	c.Assert(err, IsNil)
	err = spec.AddConnectedPlug(s.iface, s.testPlug0, s.testSlot0)
	c.Assert(err, IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app", "snap.producer.app"})

	s.checkSlotSeccompSnippet(c, spec)
	plugSnippet := spec.SnippetForTag("snap.consumer.app")
	c.Check(plugSnippet, testutil.Contains, "mq_open")
	c.Check(plugSnippet, testutil.Contains, "mq_notify")
	c.Check(plugSnippet, testutil.Contains, "mq_timedreceive")
	c.Check(plugSnippet, testutil.Contains, "mq_timedsend")
	c.Check(plugSnippet, testutil.Contains, "mq_getsetattr")
	c.Check(plugSnippet, Not(testutil.Contains), "mq_unlink")
}

func (s *PosixMQInterfaceSuite) TestDefaultReadWriteMQAppArmor(c *C) {
	spec := &apparmor.Specification{}
	err := spec.AddPermanentSlot(s.iface, s.testSlotInfo1)
	c.Assert(err, IsNil)
	err = spec.AddConnectedPlug(s.iface, s.testPlug1, s.testSlot1)
	c.Assert(err, IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app", "snap.producer.app"})

	slotSnippet := spec.SnippetForTag("snap.producer.app")
	c.Check(slotSnippet, testutil.Contains, `mqueue (open read write create delete) "/test-default",`)

	plugSnippet := spec.SnippetForTag("snap.consumer.app")
	c.Check(plugSnippet, testutil.Contains, `mqueue (read write open) "/test-default",`)
}

func (s *PosixMQInterfaceSuite) TestDefaultReadWriteMQSeccomp(c *C) {
	spec := &seccomp.Specification{}
	err := spec.AddPermanentSlot(s.iface, s.testSlotInfo1)
	c.Assert(err, IsNil)
	err = spec.AddConnectedPlug(s.iface, s.testPlug1, s.testSlot1)
	c.Assert(err, IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app", "snap.producer.app"})

	s.checkSlotSeccompSnippet(c, spec)

	plugSnippet := spec.SnippetForTag("snap.consumer.app")
	c.Check(plugSnippet, testutil.Contains, "mq_open")
	c.Check(plugSnippet, testutil.Contains, "mq_notify")
	c.Check(plugSnippet, testutil.Contains, "mq_timedreceive")
	c.Check(plugSnippet, testutil.Contains, "mq_timedsend")
	c.Check(plugSnippet, testutil.Contains, "mq_getsetattr")
	c.Check(plugSnippet, Not(testutil.Contains), "mq_unlink")
}

func (s *PosixMQInterfaceSuite) TestReadOnlyMQAppArmor(c *C) {
	spec := &apparmor.Specification{}
	err := spec.AddPermanentSlot(s.iface, s.testSlotInfo2)
	c.Assert(err, IsNil)
	err = spec.AddConnectedPlug(s.iface, s.testPlug2, s.testSlot2)
	c.Assert(err, IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app", "snap.producer.app"})

	slotSnippet := spec.SnippetForTag("snap.producer.app")
	c.Check(slotSnippet, testutil.Contains, `mqueue (open read write create delete) "/test-ro",`)

	plugSnippet := spec.SnippetForTag("snap.consumer.app")
	c.Check(plugSnippet, testutil.Contains, `mqueue (read open) "/test-ro",`)
}

func (s *PosixMQInterfaceSuite) TestReadOnlyMQSeccomp(c *C) {
	spec := &seccomp.Specification{}
	err := spec.AddPermanentSlot(s.iface, s.testSlotInfo2)
	c.Assert(err, IsNil)
	err = spec.AddConnectedPlug(s.iface, s.testPlug2, s.testSlot2)
	c.Assert(err, IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app", "snap.producer.app"})

	s.checkSlotSeccompSnippet(c, spec)

	plugSnippet := spec.SnippetForTag("snap.consumer.app")
	c.Check(plugSnippet, testutil.Contains, "mq_open")
	c.Check(plugSnippet, testutil.Contains, "mq_notify")
	c.Check(plugSnippet, testutil.Contains, "mq_timedreceive")
	c.Check(plugSnippet, testutil.Contains, "mq_getsetattr")
	c.Check(plugSnippet, Not(testutil.Contains), "mq_timedsend")
	c.Check(plugSnippet, Not(testutil.Contains), "mq_unlink")
}

func (s *PosixMQInterfaceSuite) TestAllPermsMQAppArmor(c *C) {
	spec := &apparmor.Specification{}
	err := spec.AddPermanentSlot(s.iface, s.testSlotInfo3)
	c.Assert(err, IsNil)
	err = spec.AddConnectedPlug(s.iface, s.testPlug3, s.testSlot3)
	c.Assert(err, IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app", "snap.producer.app"})

	slotSnippet := spec.SnippetForTag("snap.producer.app")
	c.Check(slotSnippet, testutil.Contains, `mqueue (open read write create delete) "/test-all-perms",`)

	plugSnippet := spec.SnippetForTag("snap.consumer.app")
	c.Check(plugSnippet, testutil.Contains, `mqueue (create delete read write open) "/test-all-perms",`)
}

func (s *PosixMQInterfaceSuite) TestAllPermsMQSeccomp(c *C) {
	spec := &seccomp.Specification{}
	err := spec.AddPermanentSlot(s.iface, s.testSlotInfo3)
	c.Assert(err, IsNil)
	err = spec.AddConnectedPlug(s.iface, s.testPlug3, s.testSlot3)
	c.Assert(err, IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app", "snap.producer.app"})

	s.checkSlotSeccompSnippet(c, spec)

	plugSnippet := spec.SnippetForTag("snap.consumer.app")
	c.Check(plugSnippet, testutil.Contains, "mq_open")
	c.Check(plugSnippet, testutil.Contains, "mq_unlink")
	c.Check(plugSnippet, testutil.Contains, "mq_getsetattr")
	c.Check(plugSnippet, testutil.Contains, "mq_notify")
	c.Check(plugSnippet, testutil.Contains, "mq_timedreceive")
	c.Check(plugSnippet, testutil.Contains, "mq_timedsend")
}

func (s *PosixMQInterfaceSuite) TestPathValidationPosixMQ(c *C) {
	spec := &apparmor.Specification{}
	err := spec.AddPermanentSlot(s.iface, s.testSlotInfo4)
	c.Assert(err, NotNil)
}

func (s *PosixMQInterfaceSuite) TestPathValidationAppArmorRegex(c *C) {
	spec := &apparmor.Specification{}
	err := spec.AddPermanentSlot(s.iface, s.testSlotInfo5)
	c.Assert(err, NotNil)
}

func (s *PosixMQInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "posix-mq")
}

func (s *PosixMQInterfaceSuite) TestSanitizeSlot(c *C) {
	restore := apparmor_sandbox.MockFeatures([]string{}, nil, []string{"mqueue"}, nil)
	defer restore()

	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.testSlotInfo0), IsNil)
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.testSlotInfo1), IsNil)
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.testSlotInfo2), IsNil)
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.testSlotInfo3), IsNil)

	/* These should return errors due to invalid paths */
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.testSlotInfo4), NotNil)
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.testSlotInfo5), NotNil)
}

func (s *PosixMQInterfaceSuite) TestSanitizePlug(c *C) {
	restore := apparmor_sandbox.MockFeatures([]string{}, nil, []string{"mqueue"}, nil)
	defer restore()

	c.Assert(interfaces.BeforePreparePlug(s.iface, s.testPlugInfo0), IsNil)
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.testPlugInfo1), IsNil)
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.testPlugInfo2), IsNil)
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.testPlugInfo3), IsNil)
}

func (s *PosixMQInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}

func (s *PosixMQInterfaceSuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(s.testPlugInfo0, s.testSlotInfo0), Equals, true)
}

func (s *PosixMQInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Check(si.ImplicitOnCore, Equals, false)
	c.Check(si.ImplicitOnClassic, Equals, false)
	c.Check(si.Summary, Equals, `allows access to POSIX message queues`)
}
