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

package ltschannel

import (
	"errors"
	"fmt"
)

// ErrLTSInternal is matched by errors.Is for programming, I/O, or parse failures
// in LTS channel resolution.
var ErrLTSInternal = errors.New("LTS internal error")

// LTSInternalError is returned when LTS channel resolution fails for an internal
// reason. errors.Is matches ErrLTSInternal.
type LTSInternalError struct{ Msg string }

func (e *LTSInternalError) Error() string { return fmt.Sprintf("internal error: %s", e.Msg) }

func (e *LTSInternalError) Is(target error) bool { return target == ErrLTSInternal }

// ErrLTSNotAllowed is matched by errors.Is when LTS policy does not allow
// channel resolution for the model.
var ErrLTSNotAllowed = errors.New("LTS not allowed")

// LTSNotAllowedError is returned when LTS policy rejects channel resolution for
// the model. errors.Is matches ErrLTSNotAllowed.
type LTSNotAllowedError struct{ Msg string }

func (e *LTSNotAllowedError) Error() string { return e.Msg }

func (e *LTSNotAllowedError) Is(target error) bool { return target == ErrLTSNotAllowed }

// ErrLTSNoTrack is matched by errors.Is when the input track has no LTS mapping
// for the model's managed boot base.
var ErrLTSNoTrack = errors.New("LTS no track")

// LTSNoTrackError is returned when the input track is not in the LTS allow-list
// for the model's managed boot base. errors.Is matches ErrLTSNoTrack.
type LTSNoTrackError struct{ Msg string }

func (e *LTSNoTrackError) Error() string { return e.Msg }

func (e *LTSNoTrackError) Is(target error) bool { return target == ErrLTSNoTrack }
