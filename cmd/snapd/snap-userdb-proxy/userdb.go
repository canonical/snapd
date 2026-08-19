// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"syscall"

	"github.com/snapcore/snapd/logger"
	"github.com/varlink/go/varlink"
)

// interfaceName is the varlink interface name advertised by the proxy
// (see https://systemd.io/USER_GROUP_API/).
const interfaceName = "io.systemd.UserDatabase"
const serviceName = "io.snapcraft.UserDBProxy"

// interfaceDescription is the varlink IDL for io.systemd.UserDatabase.
const interfaceDescription = `# Interface for querying user records and group records.
# See https://systemd.io/USER_GROUP_API/ for details.
interface io.systemd.UserDatabase

method GetUserRecord(
        uid: ?int,
        userName: ?string,
        service: string
) -> (
        record: object,
        incomplete: bool
)

method GetGroupRecord(
        gid: ?int,
        groupName: ?string,
        service: string
) -> (
        record: object,
        incomplete: bool
)

method GetMemberships(
        userName: ?string,
        groupName: ?string,
        service: string
) -> (
        userName: string,
        groupName: string
)

error NoRecordFound()
error BadService()
error ServiceNotAvailable()
error ConflictingRecordFound()
error EnumerationNotSupported()
`

// userDBIface implements the varlink dispatcher interface for
// io.systemd.UserDatabase.
type userDBIface struct {
	targetAddress string
}

func (*userDBIface) VarlinkGetName() string        { return interfaceName }
func (*userDBIface) VarlinkGetDescription() string { return interfaceDescription }

func (iface *userDBIface) VarlinkDispatch(ctx context.Context, call varlink.Call, methodName string) error {
	switch methodName {
	case "GetUserRecord", "GetGroupRecord", "GetMemberships":
		return iface.forward(ctx, call, methodName)
	default:
		return call.ReplyMethodNotFound(ctx, methodName)
	}
}

func (iface *userDBIface) forward(ctx context.Context, inbound varlink.Call, methodName string) error {
	logger.Noticef("request:\n%v\n", string(*inbound.Request))
	var params map[string]json.RawMessage
	if inbound.In.Parameters != nil {
		if err := json.Unmarshal(*inbound.In.Parameters, &params); err != nil {
			return inbound.ReplyInvalidParameter(ctx, "parameters")
		}
	}

	raw, ok := params["service"]
	if !ok {
		return inbound.ReplyError(ctx, interfaceName+".BadService", nil)
	}

	var svc string
	if err := json.Unmarshal(raw, &svc); err != nil {
		return inbound.ReplyInvalidParameter(ctx, "parameters")
	}

	if svc != serviceName {
		return inbound.ReplyError(ctx, interfaceName+".BadService", nil)
	}

	// "service" must match the service being contacted so we need to rewrite it
	params["service"] = []byte(`"io.systemd.Multiplexer"`)

	outbound, err := varlink.NewConnection(ctx, iface.targetAddress)
	if err != nil {
		if multiplexerUnavailable(err) {
			return inbound.ReplyError(ctx, interfaceName+".ServiceNotAvailable", nil)
		}
		return fmt.Errorf("cannot connect to Multiplexer: %v", err)
	}
	defer outbound.Close()

	// TODO: do we need to support oneway?
	var flags uint64
	if inbound.WantsMore() {
		flags |= varlink.More
	}

	receive, err := outbound.Send(ctx, interfaceName+"."+methodName, params, flags)
	if err != nil {
		return fmt.Errorf("cannot forward request to Multiplexer: %v", err)
	}

	for {
		var raw json.RawMessage
		outFlags, err := receive(ctx, &raw)
		if err != nil {
			return replyForwardedError(ctx, inbound, err)
		}
		logger.Noticef("response:\n%v\n", string(raw))

		// NOTE: assuming this is running as nobody, we don't need to manually strip
		// any privileged info from the response. If we depend on this assumption
		// maybe we should explicitly check and fail if it's not met
		inbound.Continues = outFlags&varlink.Continues != 0
		if err := inbound.Reply(ctx, &raw); err != nil {
			return err
		}

		if !inbound.WantsMore() || outFlags&varlink.Continues == 0 {
			break
		}
	}

	return nil
}

func replyForwardedError(ctx context.Context, call varlink.Call, err error) error {
	var verr *varlink.Error
	if errors.As(err, &verr) {
		return call.ReplyError(ctx, verr.Name, verr.Parameters)
	}
	return call.ReplyError(ctx, interfaceName+".ServiceNotAvailable", nil)
}

func multiplexerUnavailable(err error) bool {
	return errors.Is(err, syscall.ENOENT) ||
		errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.ENOTSOCK)
}
