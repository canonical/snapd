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

package seclog

import (
	"sync/atomic"

	"github.com/snapcore/snapd/testutil"
)

type SyscallOps = syscallOps

var NlmsgAlign = nlmsgAlign

const AuditTrustedApp = auditTrustedApp

func AuditWriterBuildMessage(aw *AuditWriter, payload []byte) []byte {
	return aw.buildMessage(payload)
}

func AuditWriterSetSeq(aw *AuditWriter, val uint32) {
	atomic.StoreUint32(&aw.seq, val)
}

func MockSyscallOps(ops syscallOps) (restore func()) {
	return testutil.Mock(&sys, ops)
}
