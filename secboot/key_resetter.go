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

package secboot

import (
	"fmt"
)

type MockKeyResetter struct {
	finished bool
}

type MockKeyDataWriter struct {
}

func (kdw *MockKeyDataWriter) Write(p []byte) (n int, err error) {
	return len(p), nil
}

func (kdw *MockKeyDataWriter) Commit() error {
	return nil
}

func (kr *MockKeyResetter) AddKey(slotName string, newKey []byte, token bool) (KeyDataWriter, error) {
	if kr.finished {
		return nil, fmt.Errorf("internal error: key resetter was a already finished")
	}

	if token {
		return &MockKeyDataWriter{}, nil
	} else {
		return nil, nil
	}
}

func (kr *MockKeyResetter) RemoveInstallationKey() error {
	kr.finished = true
	return nil
}
