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

package daemon

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/fdestate"
)

var systemSecurebootCmd = &Command{
	// TODO GET returning whether secure boot is relevant for the system?

	Path: "/v2/system-secureboot",
	POST: postSystemSecurebootAction,
	WriteAccess: interfaceProviderRootAccess{
		// TODO find a specialized interface for this, but for now assume that
		// requests will come only from snaps plugging fwupd interface on the
		// slot side, which also allows manipulation of EFI variables
		Interfaces: []string{"fwupd"},
	},
}

func postSystemSecurebootAction(c *Command, r *http.Request, user *auth.UserState) Response {
	contentType := r.Header.Get("Content-Type")

	switch contentType {
	case "application/json":
		return postSystemSecurebootActionJSON(c, r)
	default:
		return BadRequest("unexpected content type: %q", contentType)
	}
}

type securebootRequest struct {
	Action string `json:"action,omitempty"`

	// Payload is a base64 encoded binary blob, is used in
	// efi-secureboot-db-prepare action, and carries the DBX update content. The
	// blob is in the range from few kB to tens of kBs
	Payload string `json:"payload,omitempty"`

	// KeyDatabase is used with efi-secureboot-db-prepare action, and indicates the
	// secureboot keys database which is a target of the action, possible values are
	// PK, KEK, DB, DBX
	KeyDatabase string `json:"key-database,omitempty"`
}

func (r *securebootRequest) Validate() error {
	switch r.Action {
	case "efi-secureboot-update-startup", "efi-secureboot-update-db-cleanup":
		if r.KeyDatabase != "" {
			return fmt.Errorf("unexpected key database for action %q", r.Action)
		}

		if len(r.Payload) > 0 {
			return fmt.Errorf("unexpected payload for action %q", r.Action)
		}
	case "efi-secureboot-update-db-prepare":
		switch r.KeyDatabase {
		case "PK", "KEK", "DB", "DBX":
		default:
			return fmt.Errorf("invalid key database %q", r.KeyDatabase)
		}

		if len(r.Payload) == 0 {
			return errors.New("update payload not provided")
		}
	default:
		return fmt.Errorf("unsupported EFI secure boot action %q", r.Action)
	}
	return nil
}

func postSystemSecurebootActionJSON(c *Command, r *http.Request) Response {
	var req securebootRequest

	decoder := json.NewDecoder(r.Body)

	if err := decoder.Decode(&req); err != nil {
		return BadRequest("cannot decode request body: %v", err)
	}

	if decoder.More() {
		return BadRequest("extra content found in request body")
	}

	if err := req.Validate(); err != nil {
		return BadRequest(err.Error())
	}

	switch req.Action {
	case "efi-secureboot-update-startup":
		return postSystemActionEFISecurebootUpdateStartup(c)
	case "efi-secureboot-update-db-cleanup":
		return postSystemActionEFISecurebootUpdateDBCleanup(c)
	case "efi-secureboot-update-db-prepare":
		return postSystemActionEFISecurebootUpdateDBPrepare(c, &req)
	default:
		return InternalError("support for EFI secure boot action %q is not implemented", req.Action)
	}
}

var fdestateEFISecureBootDBUpdatePrepare = fdestate.EFISecureBootDBUpdatePrepare

func postSystemActionEFISecurebootUpdateDBPrepare(c *Command, req *securebootRequest) Response {
	if req.KeyDatabase != "DBX" {
		return InternalError("support for key database %q is not implemented", req.KeyDatabase)
	}

	payload, err := base64.StdEncoding.DecodeString(req.Payload)
	if err != nil {
		return BadRequest("cannot decode payload: %v", err)
	}

	err = fdestateEFISecureBootDBUpdatePrepare(c.d.state,
		fdestate.EFISecurebootDBX, // only DBX updates are supported
		payload)
	if err != nil {
		return BadRequest("cannot notify of update prepare: %v", err)
	}

	return SyncResponse(nil)
}

var fdestateEFISecureBootDBUpdateCleanup = fdestate.EFISecureBootDBUpdateCleanup

func postSystemActionEFISecurebootUpdateDBCleanup(c *Command) Response {
	if err := fdestateEFISecureBootDBUpdateCleanup(c.d.state); err != nil {
		return BadRequest("cannot notify of update cleanup: %v", err)
	}

	return SyncResponse(nil)
}

var fdestateEFISecureBootDBManagerStartup = fdestate.EFISecureBootDBManagerStartup

func postSystemActionEFISecurebootUpdateStartup(c *Command) Response {
	if err := fdestateEFISecureBootDBManagerStartup(c.d.state); err != nil {
		return BadRequest("cannot notify of manager startup: %v", err)
	}

	return SyncResponse(nil)
}
