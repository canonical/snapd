// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

package daemon_test

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/device"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/fdestate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/secboot/keys"
)

type systemVolumesSuite struct {
	apiBaseSuite
}

var _ = Suite(&systemVolumesSuite{})

func (s *systemVolumesSuite) SetUpTest(c *C) {
	s.apiBaseSuite.SetUpTest(c)

	s.expectRootAccess()
}

func (s *systemVolumesSuite) TestSystemVolumesBadContentType(c *C) {
	s.daemon(c)

	body := strings.NewReader(`{"action": "blah"}`)
	req, err := http.NewRequest("POST", "/v2/system-volumes", body)
	c.Assert(err, IsNil)

	rsp := s.errorReq(c, req, nil)
	c.Assert(rsp.Status, Equals, 400)
	c.Assert(rsp.Message, Equals, `unexpected content type: ""`)
}

func (s *systemVolumesSuite) TestSystemVolumesBogusAction(c *C) {
	s.daemon(c)

	body := strings.NewReader(`{"action": "blah"}`)
	req, err := http.NewRequest("POST", "/v2/system-volumes", body)
	c.Assert(err, IsNil)
	req.Header.Add("Content-Type", "application/json")

	rsp := s.errorReq(c, req, nil)
	c.Assert(rsp.Status, Equals, 400)
	c.Assert(rsp.Message, Equals, `unsupported system volumes action "blah"`)
}

func (s *systemVolumesSuite) TestSystemVolumesActionGenerateRecoveryKey(c *C) {
	if (keys.RecoveryKey{}).String() == "not-implemented" {
		c.Skip("needs working secboot recovery key")
	}

	s.daemon(c)

	called := 0
	s.AddCleanup(daemon.MockFdeMgrGenerateRecoveryKey(func(fdemgr *fdestate.FDEManager) (rkey keys.RecoveryKey, keyID string, err error) {
		called++
		c.Assert(fdemgr, NotNil)
		return keys.RecoveryKey{'r', 'e', 'c', 'o', 'v', 'e', 'r', 'y', '1', '1', '1', '1', '1', '1', '1', '1'}, "key-id-1", nil
	}))

	body := strings.NewReader(`{"action": "generate-recovery-key"}`)
	req, err := http.NewRequest("POST", "/v2/system-volumes", body)
	c.Assert(err, IsNil)
	req.Header.Add("Content-Type", "application/json")

	rsp := s.syncReq(c, req, nil)
	c.Assert(rsp.Status, Equals, 200)
	c.Check(rsp.Result, DeepEquals, map[string]string{
		"recovery-key": "25970-28515-25974-31090-12593-12593-12593-12593",
		"key-id":       "key-id-1",
	})

	c.Check(called, Equals, 1)
}

func (s *systemVolumesSuite) TestSystemVolumesActionCheckRecoveryKey(c *C) {
	if (keys.RecoveryKey{}).String() == "not-implemented" {
		c.Skip("needs working secboot recovery key")
	}

	d := s.daemon(c)

	called := 0
	s.AddCleanup(daemon.MockFdeMgrCheckRecoveryKey(func(fdemgr *fdestate.FDEManager, rkey keys.RecoveryKey, containerRoles []string) (err error) {
		called++
		// check that state is locked before calling
		d.Overlord().State().Unlock()
		d.Overlord().State().Lock()
		c.Check(rkey, DeepEquals, keys.RecoveryKey{'r', 'e', 'c', 'o', 'v', 'e', 'r', 'y', '1', '1', '1', '1', '1', '1', '1', '1'})
		c.Check(containerRoles, DeepEquals, []string{"system-data"})
		return nil
	}))

	body := strings.NewReader(`
{
	"action": "check-recovery-key",
	"recovery-key": "25970-28515-25974-31090-12593-12593-12593-12593",
	"container-roles": ["system-data"]
}`)
	req, err := http.NewRequest("POST", "/v2/system-volumes", body)
	c.Assert(err, IsNil)
	req.Header.Add("Content-Type", "application/json")

	rsp := s.syncReq(c, req, nil)
	c.Assert(rsp.Status, Equals, 200)

	c.Check(called, Equals, 1)
}

func (s *systemVolumesSuite) TestSystemVolumesActionCheckRecoveryKeyMissingKey(c *C) {
	if (keys.RecoveryKey{}).String() == "not-implemented" {
		c.Skip("needs working secboot recovery key")
	}

	s.daemon(c)

	body := strings.NewReader(`{"action": "check-recovery-key"}`)
	req, err := http.NewRequest("POST", "/v2/system-volumes", body)
	c.Assert(err, IsNil)
	req.Header.Add("Content-Type", "application/json")

	rsp := s.errorReq(c, req, nil)
	c.Assert(rsp.Status, Equals, 400)
	c.Assert(rsp.Message, Equals, "system volume action requires recovery-key to be provided")
}

func (s *systemVolumesSuite) TestSystemVolumesActionCheckRecoveryKeyBadRecoveryKeyFormat(c *C) {
	if (keys.RecoveryKey{}).String() == "not-implemented" {
		c.Skip("needs working secboot recovery key")
	}

	s.daemon(c)

	called := 0
	s.AddCleanup(daemon.MockFdeMgrCheckRecoveryKey(func(fdemgr *fdestate.FDEManager, rkey keys.RecoveryKey, containerRoles []string) (err error) {
		called++
		return nil
	}))

	body := strings.NewReader(`{"action": "check-recovery-key", "recovery-key": "aa"}`)
	req, err := http.NewRequest("POST", "/v2/system-volumes", body)
	c.Assert(err, IsNil)
	req.Header.Add("Content-Type", "application/json")

	rsp := s.errorReq(c, req, nil)
	c.Assert(rsp.Status, Equals, 400)
	// rest of error is coming from secboot
	c.Assert(rsp.Message, Equals, "cannot parse recovery key: incorrectly formatted: insufficient characters")

	c.Check(called, Equals, 0)
}

func (s *systemVolumesSuite) TestSystemVolumesActionCheckRecoveryKeyError(c *C) {
	if (keys.RecoveryKey{}).String() == "not-implemented" {
		c.Skip("needs working secboot recovery key")
	}

	s.daemon(c)

	called := 0
	s.AddCleanup(daemon.MockFdeMgrCheckRecoveryKey(func(fdemgr *fdestate.FDEManager, rkey keys.RecoveryKey, containerRoles []string) (err error) {
		called++
		return errors.New("boom!")
	}))

	body := strings.NewReader(`{"action": "check-recovery-key", "recovery-key": "25970-28515-25974-31090-12593-12593-12593-12593"}`)
	req, err := http.NewRequest("POST", "/v2/system-volumes", body)
	c.Assert(err, IsNil)
	req.Header.Add("Content-Type", "application/json")

	rsp := s.errorReq(c, req, nil)
	c.Assert(rsp.Status, Equals, 400)
	// rest of error is coming from secboot
	c.Assert(rsp.Message, Equals, "cannot find matching recovery key: boom!")

	c.Check(called, Equals, 1)
}

func (s *systemVolumesSuite) TestSystemVolumesActionReplaceRecoveryKey(c *C) {
	d := s.daemon(c)
	st := d.Overlord().State()

	d.Overlord().Loop()
	defer d.Overlord().Stop()

	called := 0
	s.AddCleanup(daemon.MockFdestateReplaceRecoveryKey(func(st *state.State, recoveryKeyID string, keyslots []fdestate.KeyslotRef) (*state.TaskSet, error) {
		called++
		c.Check(recoveryKeyID, Equals, "some-key-id")
		c.Check(keyslots, DeepEquals, []fdestate.KeyslotRef{
			{ContainerRole: "some-container-role", Name: "some-name"},
		})

		return state.NewTaskSet(), nil
	}))

	body := strings.NewReader(`
{
	"action": "replace-recovery-key",
	"key-id": "some-key-id",
	"keyslots": [
		{"container-role": "some-container-role", "name": "some-name"}
	]
}`)
	req, err := http.NewRequest("POST", "/v2/system-volumes", body)
	c.Assert(err, IsNil)
	req.Header.Add("Content-Type", "application/json")

	rsp := s.asyncReq(c, req, nil)
	c.Assert(rsp.Status, Equals, 202)

	st.Lock()
	chg := st.Change(rsp.Change)
	st.Unlock()
	c.Check(chg, NotNil)
	c.Check(chg.ID(), Equals, "1")
	c.Check(chg.Kind(), Equals, "replace-recovery-key")
	c.Check(called, Equals, 1)
}

func (s *systemVolumesSuite) TestSystemVolumesActionReplaceRecoveryKeyError(c *C) {
	s.daemon(c)

	s.AddCleanup(daemon.MockFdestateReplaceRecoveryKey(func(st *state.State, recoveryKeyID string, keyslots []fdestate.KeyslotRef) (*state.TaskSet, error) {
		return nil, errors.New("boom!")
	}))

	body := strings.NewReader(`{"action": "replace-recovery-key", "key-id": "some-key-id"}`)
	req, err := http.NewRequest("POST", "/v2/system-volumes", body)
	c.Assert(err, IsNil)
	req.Header.Add("Content-Type", "application/json")

	rsp := s.errorReq(c, req, nil)
	c.Assert(rsp.Status, Equals, 400)
	c.Assert(rsp.Message, Equals, "cannot replace recovery key: boom!")
}

func (s *systemVolumesSuite) TestSystemVolumesActionReplaceRecoveryKeyMissingKeyID(c *C) {
	s.daemon(c)

	body := strings.NewReader(`{"action": "replace-recovery-key"}`)
	req, err := http.NewRequest("POST", "/v2/system-volumes", body)
	c.Assert(err, IsNil)
	req.Header.Add("Content-Type", "application/json")

	rsp := s.errorReq(c, req, nil)
	c.Assert(rsp.Status, Equals, 400)
	c.Assert(rsp.Message, Equals, "system volume action requires key-id to be provided")
}

type mockKeyData struct {
	authMode     device.AuthMode
	platformName string
	roles        []string
}

// AuthMode indicates the authentication mechanisms enabled for this key data.
func (k *mockKeyData) AuthMode() device.AuthMode {
	return k.authMode
}

// PlatformName returns the name of the platform that handles this key data.
func (k *mockKeyData) PlatformName() string {
	return k.platformName
}

// Role indicates the role of this key.
func (k *mockKeyData) Roles() []string {
	return k.roles
}

func (s *systemVolumesSuite) testSystemVolumesGet(c *C, query string, expectedResult any) {
	d := s.daemon(c)

	s.AddCleanup(daemon.MockDevicestateGetVolumeStructuresWithKeyslots(func(st *state.State) ([]devicestate.VolumeStructureWithKeyslots, error) {
		// check state is locked
		d.Overlord().State().Unlock()
		d.Overlord().State().Lock()

		structures := []devicestate.VolumeStructureWithKeyslots{
			{VolumeStructure: gadget.VolumeStructure{VolumeName: "pc", Name: "BIOS Boot"}},
			{VolumeStructure: gadget.VolumeStructure{VolumeName: "pc", Name: "mbr", Role: "mbr"}},
			{VolumeStructure: gadget.VolumeStructure{VolumeName: "pc", Name: "ubuntu-boot", Role: "system-boot"}},
			{VolumeStructure: gadget.VolumeStructure{VolumeName: "pc", Name: "ubuntu-seed", Role: "system-seed"}},
			{
				VolumeStructure: gadget.VolumeStructure{VolumeName: "pc", Name: "ubuntu-data", Role: "system-data"},
				Keyslots: []fdestate.Keyslot{
					{Name: "default", ContainerRole: "system-data", Type: fdestate.KeyslotTypePlatform},
					{Name: "default-recovery", ContainerRole: "system-data", Type: fdestate.KeyslotTypeRecovery},
				},
			},
			{
				VolumeStructure: gadget.VolumeStructure{VolumeName: "pc", Name: "ubuntu-save", Role: "system-save"},
				Keyslots: []fdestate.Keyslot{
					{Name: "default-fallback", ContainerRole: "system-save", Type: fdestate.KeyslotTypePlatform},
					{Name: "default-recovery", ContainerRole: "system-save", Type: fdestate.KeyslotTypeRecovery},
				},
			},
		}
		fdestate.MockKeyslotKeyData(&structures[4].Keyslots[0], &mockKeyData{device.AuthModePIN, "tpm2", []string{"run+recover"}})
		fdestate.MockKeyslotKeyData(&structures[5].Keyslots[0], &mockKeyData{device.AuthModeNone, "tpm2", []string{"recover"}})
		return structures, nil
	}))

	req, err := http.NewRequest("GET", fmt.Sprintf("/v2/system-volumes%s", query), nil)
	c.Assert(err, IsNil)

	rsp := s.syncReq(c, req, nil)
	c.Assert(rsp.Status, Equals, 200)

	c.Check(rsp.Result, DeepEquals, expectedResult)
}

func (s *systemVolumesSuite) TestSystemVolumesGetAll(c *C) {
	const query = "" // default
	expectedResult := client.SystemVolumesResult{
		ByContainerRole: map[string]client.SystemVolumesStructureInfo{
			"mbr":         {VolumeName: "pc", Name: "mbr"},
			"system-boot": {VolumeName: "pc", Name: "ubuntu-boot"},
			"system-data": {
				VolumeName: "pc", Name: "ubuntu-data", Encrypted: true,
				Keyslots: map[string]client.KeyslotInfo{
					"default":          {Type: "platform", PlatformName: "tpm2", AuthMode: "pin", Roles: []string{"run+recover"}},
					"default-recovery": {Type: "recovery"},
				},
			},
			"system-save": {
				VolumeName: "pc", Name: "ubuntu-save", Encrypted: true,
				Keyslots: map[string]client.KeyslotInfo{
					"default-fallback": {Type: "platform", PlatformName: "tpm2", AuthMode: "none", Roles: []string{"recover"}},
					"default-recovery": {Type: "recovery"},
				},
			},
			"system-seed": {VolumeName: "pc", Name: "ubuntu-seed"},
		},
	}
	s.testSystemVolumesGet(c, query, expectedResult)
}

func (s *systemVolumesSuite) TestSystemVolumesGetByContainerRole(c *C) {
	const query = "?by-container-role=true"
	expectedResult := client.SystemVolumesResult{
		ByContainerRole: map[string]client.SystemVolumesStructureInfo{
			"mbr":         {VolumeName: "pc", Name: "mbr"},
			"system-boot": {VolumeName: "pc", Name: "ubuntu-boot"},
			"system-data": {
				VolumeName: "pc", Name: "ubuntu-data", Encrypted: true,
				Keyslots: map[string]client.KeyslotInfo{
					"default":          {Type: "platform", PlatformName: "tpm2", AuthMode: "pin", Roles: []string{"run+recover"}},
					"default-recovery": {Type: "recovery"},
				},
			},
			"system-save": {
				VolumeName: "pc", Name: "ubuntu-save", Encrypted: true,
				Keyslots: map[string]client.KeyslotInfo{
					"default-fallback": {Type: "platform", PlatformName: "tpm2", AuthMode: "none", Roles: []string{"recover"}},
					"default-recovery": {Type: "recovery"},
				},
			},
			"system-seed": {VolumeName: "pc", Name: "ubuntu-seed"},
		},
	}
	s.testSystemVolumesGet(c, query, expectedResult)
}

func (s *systemVolumesSuite) TestSystemVolumesGetContainerRole(c *C) {
	const query = "?container-role=system-data&container-role=mbr"
	expectedResult := client.SystemVolumesResult{
		ByContainerRole: map[string]client.SystemVolumesStructureInfo{
			"mbr": {VolumeName: "pc", Name: "mbr"},
			"system-data": {
				VolumeName: "pc", Name: "ubuntu-data", Encrypted: true,
				Keyslots: map[string]client.KeyslotInfo{
					"default":          {Type: "platform", PlatformName: "tpm2", AuthMode: "pin", Roles: []string{"run+recover"}},
					"default-recovery": {Type: "recovery"},
				},
			},
		},
	}
	s.testSystemVolumesGet(c, query, expectedResult)
}

func (s *systemVolumesSuite) TestSystemVolumesGetQueryConflictError(c *C) {
	s.daemon(c)

	req, err := http.NewRequest("GET", "/v2/system-volumes?by-container-role=true&container-role=mbr", nil)
	c.Assert(err, IsNil)

	rsp := s.errorReq(c, req, nil)
	c.Assert(rsp.Status, Equals, 400)
	c.Assert(rsp.Message, Equals, `"container-role" query parameter conflicts with "by-container-role"`)
}

func (s *systemVolumesSuite) TestSystemVolumesGetQueryByContainerRoleError(c *C) {
	s.daemon(c)

	req, err := http.NewRequest("GET", "/v2/system-volumes?by-container-role=ok", nil)
	c.Assert(err, IsNil)

	rsp := s.errorReq(c, req, nil)
	c.Assert(rsp.Status, Equals, 400)
	c.Assert(rsp.Message, Equals, `"by-container-role" query parameter when used must be set to "true" or "false" or left unset`)
}

func (s *systemVolumesSuite) TestSystemVolumesGetGadgetError(c *C) {
	s.daemon(c)

	s.AddCleanup(daemon.MockDevicestateGetVolumeStructuresWithKeyslots(func(st *state.State) ([]devicestate.VolumeStructureWithKeyslots, error) {
		return nil, errors.New("boom!")
	}))

	req, err := http.NewRequest("GET", "/v2/system-volumes", nil)
	c.Assert(err, IsNil)

	rsp := s.errorReq(c, req, nil)
	c.Assert(rsp.Status, Equals, 500)
	c.Assert(rsp.Message, Equals, "cannot get encryption information for gadget volumes: boom!")
}
