// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

package internal

import (
	"bytes"
	"fmt"

	"github.com/snapcore/snapd/snap/quota"
)

func formatJournalSizeConf(grp *quota.Group) string {
	if grp.JournalLimit.Size == 0 {
		return ""
	}
	return fmt.Sprintf(`SystemMaxUse=%[1]d
RuntimeMaxUse=%[1]d
`, grp.JournalLimit.Size)
}

func formatJournalRateConf(grp *quota.Group) string {
	if !grp.JournalLimit.RateEnabled {
		return ""
	}
	return fmt.Sprintf(`RateLimitIntervalSec=%dus
RateLimitBurst=%d
`, grp.JournalLimit.RatePeriod.Microseconds(), grp.JournalLimit.RateCount)
}

func GenerateQuotaJournaldConfFile(grp *quota.Group) []byte {
	if grp.JournalLimit == nil {
		return nil
	}

	sizeOptions := formatJournalSizeConf(grp)
	rateOptions := formatJournalRateConf(grp)
	// Set Storage=auto for all journal namespaces we create. This is
	// the setting for the default namespace, and 'persistent' is the default
	// setting for all namespaces. However we want namespaces to honor the
	// journal.persistent setting, and this only works if Storage is set
	// to 'auto'.
	// See https://www.freedesktop.org/software/systemd/man/journald.conf.html#Storage=
	template := `# Journald configuration for snap quota group %[1]s
[Journal]
Storage=auto
`
	buf := bytes.Buffer{}
	fmt.Fprintf(&buf, template, grp.Name)
	fmt.Fprint(&buf, sizeOptions, rateOptions)
	return buf.Bytes()
}

func GenerateQuotaJournalServiceFile(grp *quota.Group) []byte {
	buf := bytes.Buffer{}
	template := `[Service]
LogsDirectory=
`
	fmt.Fprint(&buf, template)
	return buf.Bytes()
}
