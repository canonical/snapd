// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build nosecboot

/*
 * Copyright (C) 2025 Canonical Ltd
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
	"context"
)

type PreinstallCheckContext struct{}

func PreinstallCheck(ctx context.Context, bootImagePaths []string) (*PreinstallCheckContext, []PreinstallErrorDetails, error) {
	return nil, nil, errBuildWithoutSecboot
}

func (c *PreinstallCheckContext) PreinstallCheckAction(ctx context.Context, action *PreinstallAction) ([]PreinstallErrorDetails, error) {
	return nil, errBuildWithoutSecboot
}

func (c *PreinstallCheckContext) SaveCheckInfo(filename string) error {
	return errBuildWithoutSecboot
}
