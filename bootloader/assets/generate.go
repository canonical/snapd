// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package assets

//go:generate go run $GOINVOKEFLAGS ./genasset/main.go -name grub.cfg -in ./data/grub.cfg -out ./grub_cfg_asset.go
//go:generate go run $GOINVOKEFLAGS ./genasset/main.go -name grub-recovery.cfg -in ./data/grub-recovery.cfg -out ./grub_recovery_cfg_asset.go
