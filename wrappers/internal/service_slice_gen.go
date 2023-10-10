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
	"runtime"

	"github.com/snapcore/snapd/snap/quota"
	"github.com/snapcore/snapd/strutil"
)

func min(a, b int) int {
	if b < a {
		return b
	}
	return a
}

func formatCpuGroupSlice(grp *quota.Group) string {
	header := `# Always enable cpu accounting, so the following cpu quota options have an effect
CPUAccounting=true
`
	buf := bytes.NewBufferString(header)

	count, percentage := grp.GetLocalCPUQuota()
	if percentage != 0 {
		// convert the number of cores and the allowed percentage
		// to the systemd specific format.
		cpuQuotaSnap := count * percentage
		cpuQuotaMax := runtime.NumCPU() * 100

		// The CPUQuota setting is only available since systemd 213
		fmt.Fprintf(buf, "CPUQuota=%d%%\n", min(cpuQuotaSnap, cpuQuotaMax))
	}

	if grp.CPULimit != nil && len(grp.CPULimit.CPUSet) != 0 {
		allowedCpusValue := strutil.IntsToCommaSeparated(grp.CPULimit.CPUSet)
		fmt.Fprintf(buf, "AllowedCPUs=%s\n", allowedCpusValue)
	}

	buf.WriteString("\n")
	return buf.String()
}

func formatMemoryGroupSlice(grp *quota.Group) string {
	header := `# Always enable memory accounting otherwise the MemoryMax setting does nothing.
MemoryAccounting=true
`
	buf := bytes.NewBufferString(header)
	if grp.MemoryLimit != 0 {
		valuesTemplate := `MemoryMax=%[1]d
# for compatibility with older versions of systemd
MemoryLimit=%[1]d

`
		fmt.Fprintf(buf, valuesTemplate, grp.MemoryLimit)
	}
	return buf.String()
}

func formatTaskGroupSlice(grp *quota.Group) string {
	header := `# Always enable task accounting in order to be able to count the processes/
# threads, etc for a slice
TasksAccounting=true
`
	buf := bytes.NewBufferString(header)

	if grp.ThreadLimit != 0 {
		fmt.Fprintf(buf, "TasksMax=%d\n", grp.ThreadLimit)
	}
	return buf.String()
}

// GenerateQuotaSliceUnitFile generates a systemd slice unit definition for the
// specified quota group.
func GenerateQuotaSliceUnitFile(grp *quota.Group) []byte {
	buf := bytes.Buffer{}

	cpuOptions := formatCpuGroupSlice(grp)
	memoryOptions := formatMemoryGroupSlice(grp)
	taskOptions := formatTaskGroupSlice(grp)
	template := `[Unit]
Description=Slice for snap quota group %[1]s
Before=slices.target
X-Snappy=yes

[Slice]
`

	fmt.Fprintf(&buf, template, grp.Name)
	fmt.Fprint(&buf, cpuOptions, memoryOptions, taskOptions)
	return buf.Bytes()
}
