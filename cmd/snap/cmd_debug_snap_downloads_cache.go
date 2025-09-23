// -*- Mode: Go; indent-tabs-mode: t -*-

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

package main

import (
	"fmt"
	"text/tabwriter"

	"github.com/jessevdk/go-flags"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/strutil/quantity"
)

const longSnapsCacheHelp = `
Show statistics of the local snap downloads cache.
`

type cmdSnapDownloadsCache struct {
	Dir      string `long:"cache"`
	MaxItems *uint  `long:"max-items"`
}

func init() {
	addDebugCommand("snap-downloads-cache",
		"Show statistics of the local snaps download cache",
		longSnapsCacheHelp,
		func() flags.Commander {
			return &cmdSnapDownloadsCache{}
		}, map[string]string{
			"cache":     "Cache directory, if different than the default location",
			"max-items": "Maximum count of cache-unique items, if different than the default",
		}, nil)
}

func (x *cmdSnapDownloadsCache) Execute(args []string) error {
	cacheDir := dirs.SnapDownloadCacheDir
	const sameAsOverlordDefaultCacheDownloads = 5
	maxItems := sameAsOverlordDefaultCacheDownloads

	if x.Dir != "" {
		cacheDir = x.Dir
	}

	if x.MaxItems != nil {
		maxItems = int(*x.MaxItems)
	}

	cm := store.NewCacheManager(cacheDir, maxItems)
	stats, err := cm.Stats()
	if err != nil {
		return fmt.Errorf("cannot obtain cache stats: %w", err)
	}

	// TODO add ability to invoke cleanup?

	fmt.Fprintf(Stdout, "Cache location: %v\n", cacheDir)
	fmt.Fprintf(Stdout, "Max cache-unique items: %v\n", maxItems)
	fmt.Fprintf(Stdout, "Cache entries: %v\n", stats.TotalEntries)
	fmt.Fprintf(Stdout, "Total size: %v\n", quantity.FormatAmount(stats.TotalSize, -1))
	fmt.Fprintf(Stdout, "Prune candidates: %v\n", len(stats.PruneCandidates))
	if candidatesCount := len(stats.PruneCandidates); candidatesCount > 0 {
		wouldRemoveCount := 0
		tw := tabwriter.NewWriter(Stdout, 2, 2, 1, ' ', 0)

		fmt.Fprintf(tw, "Name\tSize\tMod time\tWould remove\n")
		for _, c := range stats.PruneCandidates {
			wouldRemove := "no"
			if remaining := candidatesCount - wouldRemoveCount; remaining > maxItems {
				wouldRemove = "yes"
				wouldRemoveCount++
			}

			fmt.Fprintf(tw, "%s\t%v\t%s\t%s\n",
				c.Name(),
				quantity.FormatAmount(uint64(c.Size()), -1),
				c.ModTime(),
				wouldRemove,
			)
		}
		tw.Flush()
	}

	return nil
}
