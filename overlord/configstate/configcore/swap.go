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

package configcore

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mvo5/goconfigparser"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/sysconfig"
	"github.com/snapcore/snapd/systemd"
)

func init() {
	supportedConfigurations["core.swap.size"] = true
}

func validateSystemSwapConfiguration(tr ConfGetter) error {
	output, err := coreCfg(tr, "swap.size")
	if err != nil {
		return err
	}

	if output == "" {
		return nil
	}

	// valid option for swap size is any integer multiple of a megabyte that is
	// larger than or equal to 1 MB, or 0 for no swap enabled.
	_, err = parseAndValidateSwapSize(output)
	return err
}

func parseAndValidateSwapSize(szString string) (quantity.Size, error) {
	sz, err := quantity.ParseSize(szString)
	if err != nil {
		return 0, err
	}

	switch {
	case sz < 0:
		// negative doesn't make sense
		return 0, fmt.Errorf("swap size setting must be positive size in megabytes")
	case sz > 0 && sz < quantity.SizeMiB:
		// too small
		return 0, fmt.Errorf("swap size setting must be at least one megabyte")
	case sz%quantity.SizeMiB != 0:
		// must be even number of megabytes
		return 0, fmt.Errorf("swap size setting must be an integer number of megabytes")
	}
	return sz, nil
}

func handlesystemSwapConfiguration(_ sysconfig.Device, tr ConfGetter, opts *fsOnlyContext) error {
	var pristineSwapSize, newSwapSize string
	if err := tr.GetPristine("core", "swap.size", &pristineSwapSize); err != nil && !config.IsNoOption(err) {
		return err
	}
	if err := tr.Get("core", "swap.size", &newSwapSize); err != nil && !config.IsNoOption(err) {
		return err
	}
	if pristineSwapSize == newSwapSize {
		return nil
	}

	// if it's unset, then treat it as if the size is "0" to not use swap by
	// default
	if newSwapSize == "" {
		newSwapSize = "0"
	}

	szBytes, err := parseAndValidateSwapSize(newSwapSize)
	if err != nil {
		return err
	}

	rootDir := dirs.GlobalRootDir
	if opts != nil {
		rootDir = opts.RootDir
	}

	swapConfigPath := filepath.Join(rootDir, "/etc/default/swapfile")

	// TODO: also support writing/setting the location of the swap file setting?

	// default location of the swapfile in case we can't determine the location
	// from the config file
	location := "/var/tmp/swapfile.swp"
	if osutil.FileExists(swapConfigPath) {
		// then get values from the config file
		// read the existing file to get the location setting
		cfg := goconfigparser.New()
		cfg.AllowNoSectionHeader = true

		if err := cfg.ReadFile(swapConfigPath); err != nil {
			return err
		}

		location, err = cfg.Get("", "FILE")
		if err != nil {
			return err
		}
	}

	// ensure the directory exists
	if err := os.MkdirAll(filepath.Dir(swapConfigPath), 0755); err != nil {
		return err
	}

	// the size of swap needs to be specified in Megabytes, while quantity.Size
	// is a uint64 of bytes
	fileContent := fmt.Sprintf("FILE=%s\nSIZE=%d\n", location, szBytes/quantity.SizeMiB)

	// write the swap config file
	if err := os.WriteFile(swapConfigPath, []byte(fileContent), 0644); err != nil {
		return err
	}

	if opts == nil {
		// if we are not doing this filesystem only, then we need to also
		// restart the swap service
		sysd := systemd.NewUnderRoot(dirs.GlobalRootDir, systemd.SystemMode, &backlightSysdLogger{})

		if err := sysd.Restart([]string{"swapfile.service"}); err != nil {
			return err
		}
	}

	return nil
}
