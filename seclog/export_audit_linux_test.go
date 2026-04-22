// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build go1.21 && !nonativeendian

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

package seclog

import (
	"github.com/snapcore/snapd/testutil"
)

type AuditWriter = auditWriter

type AuditSinkFactory = auditSinkFactory

type NetlinkOps = netlinkOps

var NlmsgAlign = nlmsgAlign

const NlmsghdrSize = nlmsghdrSize

const AuditTrustedApp = auditTrustedApp

func AuditWriterBuildMessage(aw *auditWriter, payload []byte) []byte {
	return aw.buildMessage(payload)
}

func AuditWriterSetPortID(aw *auditWriter, id uint32) {
	aw.portID = id
}

func MockNetlink(ops netlinkOps) (restore func()) {
	return testutil.Mock(&netlink, ops)
}
