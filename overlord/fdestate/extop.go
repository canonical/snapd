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
package fdestate

import (
	"encoding/json"
	"fmt"

	"github.com/snapcore/snapd/overlord/state"
)

// ExternalOperationStatus captures the status of operations running externally.
type ExternalOperationStatus int

// Admitted status values for changes and tasks. The expected use of status is
// shown in a diagram below:
/*
           Default
              |
              v
          Preparing
              | |
              v +---------+
            Doing         |
              |           |
       +------+-----+     |
       v            v     |
   Completing   Aborting  |
      | |           |     |
      | |           |     |
      | +-----------+-----+
      |             |
      v             v
    Done          Error
*/
// The Done and Error statuses are considered to be final. A newly created
// operation should be assigned a Preparing status and then Doing after. Doing
// is where the operation is running externally. Once notified about status
// change, the status should be changed to Completing or Aborting, proceeded by
// one of the final statues. The statuses Preparing, Completing, Aborting, are
// useful when an intermediate step is needed for properly blocking conflicting
// API calls, where a state is internally unlocked, eg. when resealing.
const (
	DefaultStatus ExternalOperationStatus = 0

	// Preparing means that we are performing preparation steps, but the
	// operation isn't yet running externally.
	PreparingStatus ExternalOperationStatus = 1

	// DoingStatus means the operation is running externally.
	DoingStatus ExternalOperationStatus = 2

	// DoneStatus means the operation has completed successfully. Done status is
	// final.
	DoneStatus ExternalOperationStatus = 3

	// CompletingStatus means the operation is completing.
	CompletingStatus ExternalOperationStatus = 4

	// AbortingStatus means the operation is aborting.
	AbortingStatus ExternalOperationStatus = 5

	// ErrorStatus means the operation has failed. Error status is final.
	ErrorStatus ExternalOperationStatus = 6
)

func (s ExternalOperationStatus) String() string {
	switch s {
	case DefaultStatus:
		return "default"
	case PreparingStatus:
		return "preparing"
	case DoingStatus:
		return "doing"
	case DoneStatus:
		return "done"
	case CompletingStatus:
		return "completing"
	case AbortingStatus:
		return "aborting"
	case ErrorStatus:
		return "error"
	}
	panic(fmt.Sprintf("internal error: unexpected external operation status code: %d", s))
}

// externalOperation captures an externally running operation that is tracked in
// the state.
type externalOperation struct {
	Kind string `json:"kind"`
	// ChangeID is ID of a state.Change associated with the operation.
	ChangeID string                  `json:"change-id"`
	Status   ExternalOperationStatus `json:"status"`
	// Err when not empty, carries the error message, usually associated with
	// the operation having an ErrorStatus or AbortingStatus.
	Err string `json:"err"`
	// Context is an opaque piece of data associated with the operation.
	Context json.RawMessage `json:"context"`
}

// SetFailed is a convenience helper, which essentially does
// SetStatus(ErrorStatus), but also sets the error description.
func (e *externalOperation) SetFailed(msg string) {
	e.Status = ErrorStatus
	e.Err = msg
}

func (e *externalOperation) SetStatus(status ExternalOperationStatus) {
	e.Status = status
}

// Equal checks the operations for equality by comparing their kind and the
// parent change ID.
func (e *externalOperation) Equal(other *externalOperation) bool {
	return e.ChangeID == other.ChangeID && e.Kind == other.Kind
}

// IsReady returns true when operation status is one of the final ones
// (ErrorStatus or DoneStatus).
func (e *externalOperation) IsReady() bool {
	switch e.Status {
	case DoneStatus, ErrorStatus:
		return true
	}

	return false
}

// findFirstPendingExternalOperationByKind attempts to find a first instance of
// an operation in the state with a given kind. If none is found, nil is
// returned, without any errors.
func findFirstPendingExternalOperationByKind(st *state.State, kind string) (foundOp *externalOperation, err error) {
	err = withFdeState(st, func(fde *FdeState) (modified bool, err error) {
		for _, op := range fde.PendingExternalOperations {
			if op.Kind == kind && !op.IsReady() {
				foundOp = &op
				break
			}
		}
		return false, nil
	})
	return foundOp, err
}

// findFirstExternalOperationByChangeID attempts to find a first instance of an
// operation tracked in the state with a given change ID. If none is found, nil
// is returned, without any errors.
func findFirstExternalOperationByChangeID(st *state.State, changeID string) (foundOp *externalOperation, err error) {
	err = withFdeState(st, func(fde *FdeState) (modified bool, err error) {
		for _, op := range fde.PendingExternalOperations {
			if op.ChangeID == changeID {
				foundOp = &op
				break
			}
		}
		return false, nil
	})
	return foundOp, err
}

// addExternalOperation adds external operation to the state. Does not perform
// any checks for duplicates.
func addExternalOperation(st *state.State, op *externalOperation) error {
	return withFdeState(st, func(fde *FdeState) (modified bool, err error) {
		fde.PendingExternalOperations = append(fde.PendingExternalOperations, *op)
		return true, nil
	})
}

// updateExternalOperation updates an external operation tracked in the state,
// replacing it with the new one. The operation uses externlOperation.Equal()
// predicate. Returns an error if no matching operation was found in the state.
func updateExternalOperation(st *state.State, upOp *externalOperation) error {
	return withFdeState(st, func(fde *FdeState) (modified bool, err error) {
		mod := false
		for idx, op := range fde.PendingExternalOperations {
			if op.Equal(upOp) {
				// shallow copy
				fde.PendingExternalOperations[idx] = *upOp
				mod = true
				break
			}
		}

		if !mod {
			return false, fmt.Errorf("no matching operation")
		}

		return true, nil
	})
}
