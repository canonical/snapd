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

package lsm

import (
	"github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/sandbox/selinux"
)

const (
	// SecurityLabelKeyAppArmor is the map key for an AppArmor security label.
	SecurityLabelKeyAppArmor = "AppArmor"
	// SecurityLabelKeySELinux is the map key for a SELinux security context.
	SecurityLabelKeySELinux = "SELinux"
)

var (
	apparmorSecurityLabelFromPid = apparmor.SecurityLabelFromPid
	selinuxSecurityLabelFromPid  = selinux.SecurityLabelFromPid
	apparmorProbedLevel          = apparmor.ProbedLevel
	selinuxProbedLevel           = selinux.ProbedLevel
)

// SecurityLabelsFromPid returns the AppArmor and/or SELinux security labels of
// the process with the given pid. Map keys are [SecurityLabelKeyAppArmor] and
// [SecurityLabelKeySELinux]; entries are omitted when the corresponding LSM is
// not in use or no label is available.
func SecurityLabelsFromPid(pid int) (map[string]string, error) {
	labels := make(map[string]string)

	if apparmorProbedLevel() != apparmor.Unsupported {
		label, err := apparmorSecurityLabelFromPid(pid)
		if err != nil {
			return nil, err
		}
		labels[SecurityLabelKeyAppArmor] = label
	}

	if selinuxProbedLevel() != selinux.Unsupported {
		label, err := selinuxSecurityLabelFromPid(pid)
		if err != nil {
			return nil, err
		}
		if label != "" {
			labels[SecurityLabelKeySELinux] = label
		}
	}

	return labels, nil
}

// SecurityLabelFromPid returns the AppArmor or SELinux LSM security label of
// the process with the given pid.
func SecurityLabelFromPid(pid int) (string, error) {
	labels, err := SecurityLabelsFromPid(pid)
	if err != nil {
		return "", err
	}
	if label, ok := labels[SecurityLabelKeyAppArmor]; ok && label != "unconfined" {
		return label, nil
	}
	if label, ok := labels[SecurityLabelKeySELinux]; ok {
		return label, nil
	}
	return "unconfined", nil
}
