// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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

package caps

import (
	. "gopkg.in/check.v1"
)

type HookSuite struct {
	cap *Capability
}

var _ = Suite(&HookSuite{
	cap: &Capability{
		Name:  "test-name",
		Label: "test-label",
		Type: &Type{
			Name: "test-type",
		},
		Attrs: map[string]string{
			"attr-name": "attr-value",
		},
	},
})

func (s *HookSuite) TestEnumerateRequest(c *C) {
	msg := makeEnumerateRequest()
	c.Assert(msg, DeepEquals, &EnumerateRequest{
		msgBase: msgBase{"enumerate-request"},
	})
}

func (s *HookSuite) TestEnumerateResponse(c *C) {
	caps := map[string]*Capability{
		s.cap.Name: s.cap,
	}
	msg := makeEnumerateResponse(caps)
	c.Assert(msg, DeepEquals, &EnumerateResponse{
		msgBase: msgBase{"enumerate-response"},
		Provides: map[string]CapabilityInfo{
			"test-name": CapabilityInfo{
				Type:  "test-type",
				Label: "test-label",
				Attributes: map[string]string{
					"attr-name": "attr-value",
				},
			},
		},
	})
}

func (s *HookSuite) TestGrantRequest(c *C) {
	msg := makeGrantRequest(s.cap)
	c.Assert(msg, DeepEquals, &GrantRequest{
		msgBase: msgBase{"grant-request"},
		Name:    "test-name",
		Type:    "test-type",
	})
}

func (s *HookSuite) TestGrantAccepted(c *C) {
	msg := makeGrantAccepted(s.cap)
	c.Assert(msg, DeepEquals, &GrantAccepted{
		msgBase: msgBase{"grant-accepted"},
		Name:    "test-name",
		Type:    "test-type",
		Label:   "test-label",
		Attributes: map[string]string{
			"attr-name": "attr-value",
		},
	})
}

func (s *HookSuite) TestGrantRejected(c *C) {
	msg := makeGrantRejected("reason", "error-message")
	c.Assert(msg, DeepEquals, &GrantRejected{
		msgBase:      msgBase{"grant-rejected"},
		Reason:       "reason",
		ErrorMessage: "error-message",
	})
}

func (s *HookSuite) TestRevokeRequest(c *C) {
	msg := makeRevokeRequest(s.cap)
	c.Assert(msg, DeepEquals, &RevokeRequest{
		msgBase: msgBase{"revoke-request"},
		Name:    "test-name",
	})
}

func (s *HookSuite) TestGrantNotification(c *C) {
	msg := makeGrantNotification(s.cap, "slot-name")
	c.Assert(msg, DeepEquals, &GrantNotification{
		msgBase: msgBase{"grant-notification"},
		Name:    "test-name",
		Type:    "test-type",
		Label:   "test-label",
		Attributes: map[string]string{
			"attr-name": "attr-value",
		},
		Slot: "slot-name",
	})
}

func (s *HookSuite) TestRevokeNotification(c *C) {
	msg := makeRevokeNotification(s.cap, "slot-name")
	c.Assert(msg, DeepEquals, &RevokeNotification{
		msgBase: msgBase{"revoke-notification"},
		Name:    "test-name",
		Slot:    "slot-name",
	})
}

func (s *HookSuite) TestOverrideRequest(c *C) {
	msg := makeOverrideRequest()
	c.Assert(msg, DeepEquals, &OverrideRequest{
		msgBase: msgBase{"override-request"},
	})
}

func (s *HookSuite) TestOverrideResponse(c *C) {
	renames := map[string]CapabilityRenameInfo{
		"old-name": CapabilityRenameInfo{
			NewName:  "new-name",
			NewLabel: "new-label",
		},
	}
	suppresses := []string{"name-*"}
	msg := makeOverrideResponse(renames, suppresses)
	c.Assert(msg, DeepEquals, &OverrideResponse{
		msgBase:    msgBase{"override-response"},
		Renames:    renames,
		Suppresses: suppresses,
	})
}
