// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

package servicestate

import (
	"fmt"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/overlord/state"
)

// An AffectedQuotasFunc returns a list of affected quota group names for the given supported task.
type AffectedQuotasFunc func(*state.Task) ([]string, error)

var affectedQuotasByKind = make(map[string]AffectedQuotasFunc)

// RegisterAffectedQuotasByKind registers an AffectedQuotasFunc for returning the affected quotas
// for tasks of the given kind, to use in conflicts detection.
func RegisterAffectedQuotasByKind(kind string, f AffectedQuotasFunc) {
	affectedQuotasByKind[kind] = f
}

// QuotaChangeConflictError represents an error because of quota group conflicts between changes.
type QuotaChangeConflictError struct {
	Quota      string
	ChangeKind string
	// a Message is optional, otherwise one is composed from the other information
	Message string
}

func (e *QuotaChangeConflictError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.ChangeKind != "" {
		return fmt.Sprintf("quota group %q has %q change in progress", e.Quota, e.ChangeKind)
	}
	return fmt.Sprintf("quota group %q has changes in progress", e.Quota)
}

// CheckQuotaChangeConflictMany ensures that for the given quota groups no other
// changes that alters them (like create, update, remove) are in
// progress. If a conflict is detected an error is returned.
func CheckQuotaChangeConflictMany(st *state.State, quotaNames []string) error {
	quotaMap := make(map[string]bool, len(quotaNames))
	for _, k := range quotaNames {
		quotaMap[k] = true
	}

	for _, task := range st.Tasks() {
		chg := task.Change()
		if chg == nil || chg.IsReady() {
			continue
		}

		quotas := mylog.Check2(affectedQuotas(task))

		for _, quota := range quotas {
			if quotaMap[quota] {
				return &QuotaChangeConflictError{Quota: quota, ChangeKind: chg.Kind()}
			}
		}
	}

	return nil
}

func affectedQuotas(t *state.Task) ([]string, error) {
	if f := affectedQuotasByKind[t.Kind()]; f != nil {
		return f(t)
	}
	return nil, nil
}
