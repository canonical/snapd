// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !linux || (!arm && !arm64) || nooptee

/*
 * Copyright (C) Canonical Ltd
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

package optee

import (
	"errors"
)

type unsupportedClient struct{}

var ErrUnsupportedPlatform = errors.New("unsupported platform")

func (c *unsupportedClient) Present() bool {
	return false
}

func (c *unsupportedClient) DecryptKey(input []byte, handle []byte) ([]byte, error) {
	return nil, ErrUnsupportedPlatform
}

func (c *unsupportedClient) EncryptKey(input []byte) (handle []byte, sealed []byte, err error) {
	return nil, nil, ErrUnsupportedPlatform
}

func (c *unsupportedClient) Lock() error {
	return ErrUnsupportedPlatform
}

func (c *unsupportedClient) Version() (string, error) {
	return "", ErrUnsupportedPlatform
}

func newFDETAClient() FDETAClient {
	return &unsupportedClient{}
}
