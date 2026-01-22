// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

package prompting_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces/prompting"
	"github.com/snapcore/snapd/sandbox/apparmor/notify"
	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) { TestingT(t) }

type promptingSuite struct {
	testutil.BaseTest
}

var _ = Suite(&promptingSuite{})

func (s *promptingSuite) SetUpTest(c *C) {
	restore := prompting.MockCgroupProcessPathInTrackingCgroup(func(pid int) (string, error) {
		return "some-cgroup-path", nil
	})
	s.AddCleanup(restore)
}

func newMsgNotificationFile(protocolVersion notify.ProtocolVersion, id uint64, label, name string, allow, deny uint32, tagsets notify.TagsetMap) *notify.MsgNotificationFile {
	msg := notify.MsgNotificationFile{}
	msg.Version = protocolVersion
	msg.NotificationType = notify.APPARMOR_NOTIF_OP
	msg.KernelNotificationID = id
	msg.Allow = allow
	msg.Deny = deny
	msg.Pid = 1234
	msg.Label = label
	msg.Class = notify.AA_CLASS_FILE
	msg.SUID = 1000
	msg.Filename = name
	msg.Tagsets = tagsets
	return &msg
}

func (s *promptingSuite) TestNewListenerRequestSimple(c *C) {
	var (
		protoVersion = notify.ProtocolVersion(2)
		id           = uint64(123)
		label        = "snap.foo.bar"
		path         = "/home/Documents/foo"
		aBits        = uint32(0b1010) // write (and append)
		dBits        = uint32(0b0101) // read, exec

		tagsets = notify.TagsetMap{
			notify.FilePermission(0b1100): notify.MetadataTags{"tag1", "tag2"},
			notify.FilePermission(0b0010): notify.MetadataTags{"tag3"},
			notify.FilePermission(0b0001): notify.MetadataTags{"tag4"},
		}

		iface         = "home"
		expectedPerms = []string{"read", "execute"}
	)

	restore := prompting.MockApparmorInterfaceForMetadataTag(func(tag string) (string, bool) {
		switch tag {
		case "tag2", "tag4":
			return iface, true
		default:
			return "", false
		}
	})
	defer restore()

	msg := newMsgNotificationFile(protoVersion, id, label, path, aBits, dBits, tagsets)

	result, err := prompting.NewListenerRequest(msg, nil)
	c.Assert(err, IsNil)
	c.Assert(result, NotNil)

	c.Check(result.Key, Equals, fmt.Sprintf("kernel:home:%016X", id))
	c.Check(result.UID, Equals, msg.SUID)
	c.Check(result.PID, Equals, msg.Pid)
	c.Check(result.Cgroup, Equals, "some-cgroup-path")
	c.Check(result.AppArmorLabel, Equals, label)
	c.Check(result.Interface, Equals, iface)
	c.Check(result.Permissions, DeepEquals, expectedPerms)
	c.Check(result.Path, Equals, path)
}

func (s *promptingSuite) TestNewListenerRequestInterfaceSelection(c *C) {
	var (
		protoVersion = notify.ProtocolVersion(2)
		id           = uint64(123)
		label        = "snap.foo.bar"
		aBits        = uint32(0b1010) // write (and append)
		dBits        = uint32(0b0101) // read, exec

		tagsets = notify.TagsetMap{
			notify.FilePermission(0b1100): notify.MetadataTags{"tag1", "tag2"},
			notify.FilePermission(0b0010): notify.MetadataTags{"tag3"},
			notify.FilePermission(0b0001): notify.MetadataTags{"tag4"},
		}
	)

	for i, testCase := range []struct {
		path          string
		ifaceForTag   func(tag string) (string, bool)
		expectedIface string
	}{
		{
			"/path/to/foo",
			func(tag string) (string, bool) {
				switch tag {
				case "tag1", "tag4":
					return "home", true
				}
				return "", false
			},
			"home",
		},
		{
			"/path/to/foo",
			func(tag string) (string, bool) {
				switch tag {
				case "tag2", "tag4":
					return "camera", true
				}
				return "", false
			},
			"camera",
		},
		{
			"/dev/video0",
			func(tag string) (string, bool) {
				switch tag {
				case "tag2", "tag4":
					return "home", true
				}
				return "", false
			},
			"home",
		},
		{
			"/home/test/foo",
			func(tag string) (string, bool) {
				switch tag {
				case "tag1", "tag4":
					return "camera", true
				}
				return "", false
			},
			"camera",
		},
		{
			"/home/test/foo",
			func(tag string) (string, bool) {
				return "", false
			},
			"home",
		},
		{
			"/dev/video5",
			func(tag string) (string, bool) {
				return "", false
			},
			"camera",
		},
	} {
		restore := prompting.MockApparmorInterfaceForMetadataTag(testCase.ifaceForTag)
		defer restore()

		msg := newMsgNotificationFile(protoVersion, id, label, testCase.path, aBits, dBits, tagsets)

		result, err := prompting.NewListenerRequest(msg, nil)

		c.Assert(err, IsNil, Commentf("testCase %d: %+v", i, testCase))
		c.Assert(result, NotNil, Commentf("testCase %d: %+v", i, testCase))
		c.Check(result.Interface, Equals, testCase.expectedIface, Commentf("testCase %d: %+v", i, testCase))
	}
}

func (s *promptingSuite) TestNewListenerRequestReply(c *C) {
	var (
		id      = uint64(0xabcd)
		version = notify.ProtocolVersion(43)
		aaAllow = uint32(0b01010000) // some higher bits
		aaDeny  = uint32(0b00110110) // read, write, some higher bits
		path    = "/home/test/foo"   // will cause "home" interface

		iface = "home"
		perms = []string{"read", "write"}
	)

	msg := &notify.MsgNotificationFile{
		MsgNotificationOp: notify.MsgNotificationOp{
			MsgNotification: notify.MsgNotification{
				MsgHeader: notify.MsgHeader{
					Length:  52,
					Version: version,
				},
				NotificationType:     notify.APPARMOR_NOTIF_OP,
				KernelNotificationID: id,
			},
			Allow: aaAllow,
			Deny:  aaDeny,
			Class: notify.AA_CLASS_FILE,
		},
		Filename: path,
	}

	// Test good replies
	for _, testCase := range []struct {
		response          []string
		expectedUserAllow notify.FilePermission
	}{
		{
			nil,
			notify.FilePermission(0),
		},
		{
			[]string{},
			notify.FilePermission(0),
		},
		{
			[]string{"read"},
			notify.AA_MAY_OPEN | prompting.InterfaceFilePermissionsMaps["home"]["read"],
		},
		{
			[]string{"write"},
			notify.AA_MAY_OPEN | prompting.InterfaceFilePermissionsMaps["home"]["write"],
		},
		{
			[]string{"read", "write"},
			notify.AA_MAY_OPEN | prompting.InterfaceFilePermissionsMaps["home"]["read"] | prompting.InterfaceFilePermissionsMaps["home"]["write"],
		},
		{
			[]string{"read", "write", "execute"},
			notify.AA_MAY_OPEN | prompting.InterfaceFilePermissionsMaps["home"]["read"] | prompting.InterfaceFilePermissionsMaps["home"]["write"] | prompting.InterfaceFilePermissionsMaps["home"]["execute"],
		},
	} {
		fakeSendResponse := func(recvID uint64, recvAaAllowed, recvAaRequested, userAllowed notify.AppArmorPermission) error {
			c.Check(recvID, Equals, id)
			c.Check(recvAaAllowed, Equals, notify.FilePermission(aaAllow))
			c.Check(recvAaRequested, Equals, notify.FilePermission(aaDeny))
			c.Check(userAllowed, Equals, testCase.expectedUserAllow)
			return nil
		}

		req, err := prompting.NewListenerRequest(msg, fakeSendResponse)
		c.Assert(err, IsNil)
		c.Assert(req, NotNil)

		c.Check(req.Interface, Equals, iface)
		c.Check(req.Permissions, DeepEquals, perms)

		err = req.Reply(testCase.response)
		c.Check(err, IsNil)
	}

	// Test bad reply
	fakeSendResponse := func(recvID uint64, recvAaAllowed, recvAaRequested, userAllowed notify.AppArmorPermission) error {
		c.Fatalf("should not have attempted to send response")
		return nil
	}
	req, err := prompting.NewListenerRequest(msg, fakeSendResponse)
	c.Assert(err, IsNil)
	c.Assert(req, NotNil)
	c.Check(req.Interface, Equals, iface)
	c.Check(req.Permissions, DeepEquals, perms)
	badPerms := []string{"read", "foo"}
	err = req.Reply(badPerms)
	c.Check(err, ErrorMatches, "cannot map abstract permission to AppArmor permissions for the home interface: \"foo\"")

	// Test error when sending response
	fakeSendResponse = func(recvID uint64, recvAaAllowed, recvAaRequested, userAllowed notify.AppArmorPermission) error {
		return fmt.Errorf("failed to send response")
	}
	req, err = prompting.NewListenerRequest(msg, fakeSendResponse)
	c.Assert(err, IsNil)
	c.Assert(req, NotNil)
	err = req.Reply([]string{"read"})
	c.Check(err, ErrorMatches, "failed to send response")
}

func (s *promptingSuite) TestNewListenerRequestErrors(c *C) {
	var (
		aBits = uint32(0b1010) // write (and append)
		dBits = uint32(0b0101) // read, exec

		tagsets = notify.TagsetMap{
			notify.FilePermission(0b1100): notify.MetadataTags{"tag1", "tag2"},
			notify.FilePermission(0b0010): notify.MetadataTags{"tag3"},
			notify.FilePermission(0b0001): notify.MetadataTags{"tag4"},
		}
	)
	for _, testCase := range []struct {
		msg         *notify.MsgNotificationFile
		prepareFunc func() (restore func())
		expectedErr string
	}{
		{
			&notify.MsgNotificationFile{
				MsgNotificationOp: notify.MsgNotificationOp{
					Class: notify.AA_CLASS_DBUS,
					Allow: aBits,
					Deny:  dBits,
				},
			},
			func() func() { return func() {} },
			"cannot decode file permissions for other mediation class: AA_CLASS_DBUS",
		},
		{
			&notify.MsgNotificationFile{
				MsgNotificationOp: notify.MsgNotificationOp{
					Pid:   int32(12345),
					Class: notify.AA_CLASS_FILE,
					Allow: aBits,
					Deny:  dBits,
				},
			},
			func() func() {
				return prompting.MockCgroupProcessPathInTrackingCgroup(func(pid int) (string, error) {
					c.Assert(pid, Equals, 12345)
					return "", fmt.Errorf("something failed")
				})
			},
			"cannot read cgroup path for request process with PID 12345: something failed",
		},
		{
			&notify.MsgNotificationFile{
				MsgNotificationOp: notify.MsgNotificationOp{
					Class: notify.AA_CLASS_FILE,
					Allow: aBits,
					Deny:  dBits,
				},
			},
			func() func() {
				return prompting.MockApparmorInterfaceForMetadataTag(func(tag string) (string, bool) {
					c.Logf("tag: %s", tag)
					switch tag {
					case "tag1":
						return "home", true
					case "tag2":
						return "camera", true
					case "tag4":
						return "audio-record", true
					}
					return "", false
				})
			},
			"cannot select interface from metadata tags: more than one interface associated with tags in request",
		},
		{
			&notify.MsgNotificationFile{
				MsgNotificationOp: notify.MsgNotificationOp{
					Class: notify.AA_CLASS_FILE,
					Allow: aBits,
					Deny:  dBits,
				},
			},
			func() func() {
				return prompting.MockApparmorInterfaceForMetadataTag(func(tag string) (string, bool) {
					switch tag {
					case "tag1":
						return "home", true
					}
					return "", false
				})
			},
			"cannot select interface from metadata tags: cannot find interface which applies to all permissions",
		},
		{
			&notify.MsgNotificationFile{
				MsgNotificationOp: notify.MsgNotificationOp{
					Class: notify.AA_CLASS_FILE,
					Allow: aBits,
					Deny:  dBits,
				},
			},
			func() func() {
				return prompting.MockApparmorInterfaceForMetadataTag(func(tag string) (string, bool) {
					return "foo", true
				})
			},
			"cannot map the given interface to list of available permissions: foo",
		},
		{
			&notify.MsgNotificationFile{
				MsgNotificationOp: notify.MsgNotificationOp{
					Class: notify.AA_CLASS_FILE,
				},
			},
			func() func() { return func() {} },
			"cannot get abstract permissions from empty AppArmor permissions: \"none\"",
		},
	} {
		restore := testCase.prepareFunc()
		testCase.msg.Tagsets = tagsets
		result, err := prompting.NewListenerRequest(testCase.msg, nil)
		c.Check(result, IsNil)
		c.Check(err, ErrorMatches, testCase.expectedErr)
		restore()
	}
}

func (s *promptingSuite) TestBuildKey(c *C) {
	for _, testCase := range []struct {
		iface    string
		id       uint64
		expected string
	}{
		{"foo", 0x1234, "kernel:foo:0000000000001234"},
		{"home", 0x1, "kernel:home:0000000000000001"},
		{"camera", 0xdeadbeefdeadbeef, "kernel:camera:DEADBEEFDEADBEEF"},
	} {
		key := prompting.BuildListenerRequestKey(testCase.iface, testCase.id)
		c.Check(key, Equals, testCase.expected)
	}
}

func (s *promptingSuite) TestIDTypeStringMarshalUnmarshalJSON(c *C) {
	for _, testCase := range []struct {
		id         prompting.IDType
		str        string
		marshalled []byte
	}{
		{0, "0000000000000000", []byte(`"0000000000000000"`)},
		{1, "0000000000000001", []byte(`"0000000000000001"`)},
		{0x1000000000000000, "1000000000000000", []byte(`"1000000000000000"`)},
		{0xDEADBEEFDEADBEEF, "DEADBEEFDEADBEEF", []byte(`"DEADBEEFDEADBEEF"`)},
		{0xFFFFFFFFFFFFFFFF, "FFFFFFFFFFFFFFFF", []byte(`"FFFFFFFFFFFFFFFF"`)},
	} {
		c.Check(testCase.id.String(), Equals, testCase.str)
		marshalled, err := json.Marshal(testCase.id)
		c.Check(err, IsNil)
		c.Check(marshalled, DeepEquals, testCase.marshalled)
		var id prompting.IDType
		err = json.Unmarshal(testCase.marshalled, &id)
		c.Check(err, IsNil)
		c.Check(id, Equals, testCase.id)
	}

	// Check that `IDType` as key in a map is marshalled correctly
	asKey := map[prompting.IDType]string{prompting.IDType(0x1234): "foo"}
	expected := []byte(`{"0000000000001234":"foo"}`)
	marshalled, err := json.Marshal(asKey)
	c.Check(err, IsNil)
	c.Check(marshalled, DeepEquals, expected, Commentf("marshalled: %s\nexpected: %s", string(marshalled), string(expected)))
	var unmarshalledAsKey map[prompting.IDType]string
	err = json.Unmarshal(marshalled, &unmarshalledAsKey)
	c.Check(err, IsNil)
	c.Check(unmarshalledAsKey, DeepEquals, asKey)

	// Check that `IDType` as value in a map is marshalled correctly
	asValue := map[string]prompting.IDType{"foo": 0x5678}
	expected = []byte(`{"foo":"0000000000005678"}`)
	marshalled, err = json.Marshal(asValue)
	c.Check(err, IsNil)
	c.Check(marshalled, DeepEquals, expected, Commentf("marshalled: %s\nexpected: %s", string(marshalled), string(expected)))
	var unmarshalledAsValue map[string]prompting.IDType
	err = json.Unmarshal(marshalled, &unmarshalledAsValue)
	c.Check(err, IsNil)
	c.Check(unmarshalledAsValue, DeepEquals, asValue)
}

func (s *promptingSuite) TestOutcomeAsBool(c *C) {
	result, err := prompting.OutcomeAllow.AsBool()
	c.Check(err, IsNil)
	c.Check(result, Equals, true)
	result, err = prompting.OutcomeDeny.AsBool()
	c.Check(err, IsNil)
	c.Check(result, Equals, false)
	_, err = prompting.OutcomeUnset.AsBool()
	c.Check(err, ErrorMatches, `invalid outcome: ""`)
	_, err = prompting.OutcomeType("foo").AsBool()
	c.Check(err, ErrorMatches, `invalid outcome: "foo"`)
}

type fakeOutcomeWrapper struct {
	Field1 prompting.OutcomeType `json:"field1"`
	Field2 prompting.OutcomeType `json:"field2,omitempty"`
}

func (s *promptingSuite) TestUnmarshalOutcomeHappy(c *C) {
	for _, outcome := range []prompting.OutcomeType{
		prompting.OutcomeAllow,
		prompting.OutcomeDeny,
	} {
		var fow1 fakeOutcomeWrapper
		data := []byte(fmt.Sprintf(`{"field1": "%s", "field2": "%s"}`, outcome, outcome))
		err := json.Unmarshal(data, &fow1)
		c.Check(err, IsNil, Commentf("data: %v", string(data)))
		c.Check(fow1.Field1, Equals, outcome, Commentf("data: %v", string(data)))
		c.Check(fow1.Field2, Equals, outcome, Commentf("data: %v", string(data)))

		var fow2 fakeOutcomeWrapper
		data = []byte(fmt.Sprintf(`{"field1": "%s"}`, outcome))
		err = json.Unmarshal(data, &fow2)
		c.Check(err, IsNil, Commentf("data: %v", string(data)))
		c.Check(fow2.Field1, Equals, outcome, Commentf("data: %v", string(data)))
		c.Check(fow2.Field2, Equals, prompting.OutcomeUnset, Commentf("data: %v", string(data)))
	}
}

func (s *promptingSuite) TestUnmarshalOutcomeUnhappy(c *C) {
	for _, outcome := range []prompting.OutcomeType{
		prompting.OutcomeUnset,
		prompting.OutcomeType("foo"),
	} {
		var fow1 fakeOutcomeWrapper
		data := []byte(fmt.Sprintf(`{"field1": "%s", "field2": "%s"}`, outcome, outcome))
		err := json.Unmarshal(data, &fow1)
		c.Check(err, ErrorMatches, fmt.Sprintf(`invalid outcome: %q`, outcome), Commentf("data: %v", string(data)))

		var fow2 fakeOutcomeWrapper
		data = []byte(fmt.Sprintf(`{"field1": "%s", "field2": "%s"}`, prompting.OutcomeAllow, outcome))
		err = json.Unmarshal(data, &fow2)
		c.Check(err, ErrorMatches, fmt.Sprintf(`invalid outcome: %q`, outcome), Commentf("data: %v", string(data)))
	}
}

type fakeLifespanWrapper struct {
	Field1 prompting.LifespanType `json:"field1"`
	Field2 prompting.LifespanType `json:"field2,omitempty"`
}

func (s *promptingSuite) TestUnmarshalLifespanHappy(c *C) {
	for _, lifespan := range []prompting.LifespanType{
		prompting.LifespanForever,
		prompting.LifespanSingle,
		prompting.LifespanTimespan,
	} {
		var flw1 fakeLifespanWrapper
		data := []byte(fmt.Sprintf(`{"field1": "%s", "field2": "%s"}`, lifespan, lifespan))
		err := json.Unmarshal(data, &flw1)
		c.Check(err, IsNil, Commentf("data: %v", string(data)))
		c.Check(flw1.Field1, Equals, lifespan, Commentf("data: %v", string(data)))
		c.Check(flw1.Field2, Equals, lifespan, Commentf("data: %v", string(data)))

		var flw2 fakeLifespanWrapper
		data = []byte(fmt.Sprintf(`{"field1": "%s"}`, lifespan))
		err = json.Unmarshal(data, &flw2)
		c.Check(err, IsNil, Commentf("data: %v", string(data)))
		c.Check(flw2.Field1, Equals, lifespan, Commentf("data: %v", string(data)))
		c.Check(flw2.Field2, Equals, prompting.LifespanUnset, Commentf("data: %v", string(data)))
	}
}

func (s *promptingSuite) TestUnmarshalLifespanUnhappy(c *C) {
	for _, lifespan := range []prompting.LifespanType{
		prompting.LifespanUnset,
		prompting.LifespanType("foo"),
	} {
		var flw1 fakeLifespanWrapper
		data := []byte(fmt.Sprintf(`{"field1": "%s", "field2": "%s"}`, lifespan, lifespan))
		err := json.Unmarshal(data, &flw1)
		c.Check(err, ErrorMatches, fmt.Sprintf(`invalid lifespan: %q`, lifespan), Commentf("data: %v", string(data)))

		var flw2 fakeLifespanWrapper
		data = []byte(fmt.Sprintf(`{"field1": "%s", "field2": "%s"}`, prompting.LifespanForever, lifespan))
		err = json.Unmarshal(data, &flw2)
		c.Check(err, ErrorMatches, fmt.Sprintf(`invalid lifespan: %q`, lifespan), Commentf("data: %v", string(data)))
	}
}

func (s *promptingSuite) TestValidateExpiration(c *C) {
	var unsetExpiration time.Time
	var unsetSession prompting.IDType
	currTime := time.Now()
	currSession := prompting.IDType(0x12345)
	negativeExpiration := currTime.Add(-5 * time.Second)
	validExpiration := currTime.Add(10 * time.Minute)

	for _, lifespan := range []prompting.LifespanType{
		prompting.LifespanForever,
		prompting.LifespanSingle,
	} {
		err := lifespan.ValidateExpiration(unsetExpiration, unsetSession)
		c.Check(err, IsNil)
		for _, exp := range []time.Time{negativeExpiration, validExpiration} {
			for _, session := range []prompting.IDType{unsetSession, currSession} {
				err = lifespan.ValidateExpiration(exp, session)
				c.Check(err, ErrorMatches, `invalid expiration: cannot have specified expiration when lifespan is.*`)
			}
		}
		err = lifespan.ValidateExpiration(unsetExpiration, currSession)
		c.Check(err, ErrorMatches, `invalid expiration: cannot have specified session ID when lifespan is.*`)
	}

	for _, exp := range []time.Time{negativeExpiration, validExpiration} {
		err := prompting.LifespanTimespan.ValidateExpiration(exp, unsetSession)
		c.Check(err, IsNil)
	}
	for _, session := range []prompting.IDType{unsetSession, currSession} {
		err := prompting.LifespanTimespan.ValidateExpiration(unsetExpiration, session)
		c.Check(err, ErrorMatches, `invalid expiration: cannot have unspecified expiration when lifespan is.*`)
	}
	err := prompting.LifespanTimespan.ValidateExpiration(validExpiration, currSession)
	c.Check(err, ErrorMatches, `invalid expiration: cannot have specified session ID when lifespan is.*`)

	err = prompting.LifespanSession.ValidateExpiration(unsetExpiration, currSession)
	c.Check(err, IsNil)
	err = prompting.LifespanSession.ValidateExpiration(unsetExpiration, unsetSession)
	c.Check(err, ErrorMatches, `invalid expiration: cannot have unspecified session ID when lifespan is.*`)
	for _, exp := range []time.Time{negativeExpiration, validExpiration} {
		for _, session := range []prompting.IDType{unsetSession, currSession} {
			err = prompting.LifespanSession.ValidateExpiration(exp, session)
			c.Check(err, ErrorMatches, `invalid expiration: cannot have specified expiration when lifespan is.*`)
		}
	}
}

func (s *promptingSuite) TestParseDuration(c *C) {
	unsetDuration := ""
	invalidDuration := "foo"
	negativeDuration := "-5s"
	validDuration := "10m"
	parsedValidDuration, err := time.ParseDuration(validDuration)
	c.Assert(err, IsNil)
	currTime := time.Now()

	for _, lifespan := range []prompting.LifespanType{
		prompting.LifespanForever,
		prompting.LifespanSingle,
	} {
		expiration, err := lifespan.ParseDuration(unsetDuration, currTime)
		c.Check(expiration.IsZero(), Equals, true)
		c.Check(err, IsNil)
		for _, dur := range []string{invalidDuration, negativeDuration, validDuration} {
			expiration, err = lifespan.ParseDuration(dur, currTime)
			c.Check(expiration.IsZero(), Equals, true)
			c.Check(err, ErrorMatches, `invalid duration: cannot have specified duration when lifespan is.*`)
		}
	}

	expiration, err := prompting.LifespanTimespan.ParseDuration(unsetDuration, currTime)
	c.Check(expiration.IsZero(), Equals, true)
	c.Check(err, ErrorMatches, `invalid duration: cannot have unspecified duration when lifespan is.*`)

	expiration, err = prompting.LifespanTimespan.ParseDuration(invalidDuration, currTime)
	c.Check(expiration.IsZero(), Equals, true)
	c.Check(err, ErrorMatches, `invalid duration: cannot parse duration.*`)

	expiration, err = prompting.LifespanTimespan.ParseDuration(negativeDuration, currTime)
	c.Check(expiration.IsZero(), Equals, true)
	c.Check(err, ErrorMatches, `invalid duration: cannot have zero or negative duration.*`)

	expiration, err = prompting.LifespanTimespan.ParseDuration(validDuration, currTime)
	c.Check(err, IsNil)
	c.Check(expiration.After(time.Now()), Equals, true)
	c.Check(expiration.Before(time.Now().Add(parsedValidDuration)), Equals, true)

	expiration2, err := prompting.LifespanTimespan.ParseDuration(validDuration, currTime)
	c.Check(err, IsNil)
	c.Check(expiration2.Equal(expiration), Equals, true)
}
