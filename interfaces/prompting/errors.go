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

package prompting

import (
	"errors"
)

var (
	// Internal errors related to backends being closed
	ErrPromptsClosed = errors.New("prompts backend has already been closed")
	ErrRulesClosed   = errors.New("rules backend has already been closed")

	// BadRequest error when an ID cannot be parsed
	ErrInvalidID = errors.New("invalid ID: format must be parsable as uint64")

	// NotFound errors when a prompt or rule is not found
	ErrPromptNotFound = errors.New("cannot find prompt with the given ID for the given user")
	ErrRuleNotFound   = errors.New("cannot find rule with the given ID")
	ErrRuleNotAllowed = errors.New("user not allowed to request the rule with the given ID")

	// Internal errors related to there being too many prompts or rules
	ErrTooManyPrompts = errors.New("cannot add new prompts, too many outstanding")

	// BadRequest errors related to bad replies or rule contents
	ErrInvalidOutcome                    = errors.New("invalid outcome")
	ErrInvalidLifespan                   = errors.New("invalid lifespan")
	ErrInvalidDuration                   = errors.New("invalid duration")
	ErrInvalidExpiration                 = errors.New("invalid expiration")
	ErrInvalidConstraints                = errors.New("invalid constraints")
	ErrRuleExpirationInThePast           = errors.New("cannot have expiration time in the past")
	ErrRuleLifespanSingle                = errors.New(`cannot create rule with lifespan "single"`)
	ErrReplyNotMatchRequestedPath        = errors.New("constraints in reply do not match originally requested path")
	ErrReplyNotMatchRequestedPermissions = errors.New("constraints in reply do not include all originally requested permissions")

	// Internal errors related to the rules backend
	ErrRuleIDConflict     = errors.New("internal error: rule with conflicting ID already exists in the rule database")
	ErrRuleDBInconsistent = errors.New("internal error: interfaces requests rule database left inconsistent")

	// Conflict error with existing rule
	ErrRuleConflict = errors.New("a rule with conflicting path pattern and permission already exists in the rule database")

	// Errors which are used internally and should never be returned over the API
	ErrNoMatchingRule = errors.New("no rule matches the given path")
)
