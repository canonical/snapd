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
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/gadget/device"
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

func (s *systemVolumesSuite) TestSystemVolumesActionCheckPassphrase(c *C) {
	s.daemon(c)

	// just mock values for output matching
	const expectedEntropy = uint32(10)
	const expectedMinEntropy = uint32(20)
	const expectedOptimalEntropy = uint32(50)

	restore := daemon.MockDeviceValidatePassphrase(func(mode device.AuthMode, s string) (*device.AuthQualityResult, error) {
		c.Check(mode, Equals, device.AuthModePassphrase)
		return &device.AuthQualityResult{
			Entropy:        expectedEntropy,
			MinEntropy:     expectedMinEntropy,
			OptimalEntropy: expectedOptimalEntropy,
		}, nil
	})
	defer restore()

	body := map[string]string{
		"action":     "check-passphrase",
		"passphrase": "this is a good passphrase",
	}

	b, err := json.Marshal(body)
	c.Assert(err, IsNil)
	buf := bytes.NewBuffer(b)
	req, err := http.NewRequest("POST", "/v2/system-volumes", buf)
	c.Assert(err, IsNil)
	req.Header.Add("Content-Type", "application/json")

	rsp := s.syncReq(c, req, nil)
	c.Assert(rsp.Status, Equals, 200)
	c.Assert(rsp.Result, DeepEquals, map[string]any{
		"entropy-bits":         uint32(10),
		"min-entropy-bits":     uint32(20),
		"optimal-entropy-bits": uint32(50),
	})
}

func (s *systemVolumesSuite) TestSystemVolumesActionCheckPassphraseError(c *C) {
	s.daemon(c)

	// just mock values for output matching
	const expectedEntropy = uint32(10)
	const expectedMinEntropy = uint32(20)
	const expectedOptimalEntropy = uint32(50)

	for _, tc := range []struct {
		passphrase string

		expectedStatus   int
		expectedErrKind  client.ErrorKind
		expectedErrMsg   string
		expectedErrValue any
	}{
		{
			passphrase:     "",
			expectedStatus: 400, expectedErrMsg: `passphrase must be provided in request body for action "check-passphrase"`,
		},
		{
			passphrase:     "bad-passphrase",
			expectedStatus: 400, expectedErrKind: "invalid-passphrase", expectedErrMsg: "passphrase did not pass quality checks",
			expectedErrValue: map[string]any{
				"reasons":              []device.AuthQualityErrorReason{device.AuthQualityErrorReasonLowEntropy},
				"entropy-bits":         expectedEntropy,
				"min-entropy-bits":     expectedMinEntropy,
				"optimal-entropy-bits": expectedOptimalEntropy,
			},
		},
	} {
		body := map[string]string{
			"action":     "check-passphrase",
			"passphrase": tc.passphrase,
		}

		restore := daemon.MockDeviceValidatePassphrase(func(mode device.AuthMode, s string) (*device.AuthQualityResult, error) {
			c.Check(mode, Equals, device.AuthModePassphrase)
			return nil, &device.AuthQualityError{
				Reasons: []device.AuthQualityErrorReason{device.AuthQualityErrorReasonLowEntropy},
				Result: device.AuthQualityResult{
					Entropy:        expectedEntropy,
					MinEntropy:     expectedMinEntropy,
					OptimalEntropy: expectedOptimalEntropy,
				},
			}
		})
		defer restore()

		b, err := json.Marshal(body)
		c.Assert(err, IsNil)
		buf := bytes.NewBuffer(b)
		req, err := http.NewRequest("POST", "/v2/system-volumes", buf)
		c.Assert(err, IsNil)
		req.Header.Add("Content-Type", "application/json")

		rspe := s.errorReq(c, req, nil)
		c.Assert(rspe.Status, Equals, tc.expectedStatus)
		c.Assert(rspe.Kind, Equals, tc.expectedErrKind)
		c.Assert(rspe.Message, Matches, tc.expectedErrMsg)
		c.Assert(rspe.Value, DeepEquals, tc.expectedErrValue)
	}
}

func (s *systemsSuite) TestSystemVolumesActionCheckPIN(c *C) {
	s.daemon(c)

	// just mock values for output matching
	const expectedEntropy = uint32(10)
	const expectedMinEntropy = uint32(20)
	const expectedOptimalEntropy = uint32(50)

	restore := daemon.MockDeviceValidatePassphrase(func(mode device.AuthMode, s string) (*device.AuthQualityResult, error) {
		c.Check(mode, Equals, device.AuthModePIN)
		return &device.AuthQualityResult{
			Entropy:        expectedEntropy,
			MinEntropy:     expectedMinEntropy,
			OptimalEntropy: expectedOptimalEntropy,
		}, nil
	})
	defer restore()

	body := map[string]string{
		"action": "check-pin",
		"pin":    "20250619",
	}

	b, err := json.Marshal(body)
	c.Assert(err, IsNil)
	buf := bytes.NewBuffer(b)
	req, err := http.NewRequest("POST", "/v2/system-volumes", buf)
	c.Assert(err, IsNil)
	req.Header.Add("Content-Type", "application/json")

	rsp := s.syncReq(c, req, nil)
	c.Assert(rsp.Status, Equals, 200)
	c.Assert(rsp.Result, DeepEquals, map[string]any{
		"entropy-bits":         uint32(10),
		"min-entropy-bits":     uint32(20),
		"optimal-entropy-bits": uint32(50),
	})
}

func (s *systemsSuite) TestSystemVolumesActionCheckPINError(c *C) {
	s.daemon(c)

	// just mock values for output matching
	const expectedEntropy = uint32(10)
	const expectedMinEntropy = uint32(20)
	const expectedOptimalEntropy = uint32(50)

	for _, tc := range []struct {
		pin string

		expectedStatus   int
		expectedErrKind  client.ErrorKind
		expectedErrMsg   string
		expectedErrValue any
	}{
		{
			pin:            "",
			expectedStatus: 400, expectedErrMsg: `pin must be provided in request body for action "check-pin"`,
		},
		{
			pin:            "0",
			expectedStatus: 400, expectedErrKind: "invalid-pin", expectedErrMsg: "PIN did not pass quality checks",
			expectedErrValue: map[string]any{
				"reasons":              []device.AuthQualityErrorReason{device.AuthQualityErrorReasonLowEntropy},
				"entropy-bits":         expectedEntropy,
				"min-entropy-bits":     expectedMinEntropy,
				"optimal-entropy-bits": expectedOptimalEntropy,
			},
		},
	} {
		body := map[string]string{
			"action": "check-pin",
			"pin":    tc.pin,
		}

		restore := daemon.MockDeviceValidatePassphrase(func(mode device.AuthMode, s string) (*device.AuthQualityResult, error) {
			c.Check(mode, Equals, device.AuthModePIN)
			return nil, &device.AuthQualityError{
				Reasons: []device.AuthQualityErrorReason{device.AuthQualityErrorReasonLowEntropy},
				Result: device.AuthQualityResult{
					Entropy:        expectedEntropy,
					MinEntropy:     expectedMinEntropy,
					OptimalEntropy: expectedOptimalEntropy,
				},
			}
		})
		defer restore()

		b, err := json.Marshal(body)
		c.Assert(err, IsNil)
		buf := bytes.NewBuffer(b)
		req, err := http.NewRequest("POST", "/v2/system-volumes", buf)
		c.Assert(err, IsNil)
		req.Header.Add("Content-Type", "application/json")

		rspe := s.errorReq(c, req, nil)
		c.Assert(rspe.Status, Equals, tc.expectedStatus)
		c.Assert(rspe.Kind, Equals, tc.expectedErrKind)
		c.Assert(rspe.Message, Matches, tc.expectedErrMsg)
		c.Assert(rspe.Value, DeepEquals, tc.expectedErrValue)
	}
}
