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
	"time"

	"github.com/jessevdk/go-flags"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/strutil/quantity"
)

const longSnapsCacheHelp = `
Show statistics of the local snap downloads cache.
`

type cmdSnapDownloadsCache struct {
	Dir          string         `long:"cache"`
	MaxItems     *uint          `long:"max-items"`
	All          bool           `long:"all"`
	MaxSizeBytes *uint64        `long:"max-size-bytes"`
	MaxAge       *time.Duration `long:"max-age"`
}

func init() {
	addDebugCommand("snap-downloads-cache",
		"Show statistics of the local snaps download cache",
		longSnapsCacheHelp,
		func() flags.Commander {
			return &cmdSnapDownloadsCache{}
		}, map[string]string{
			"cache":          "Cache directory, if different than the default location",
			"max-items":      "Maximum count of cache-unique items, if different than the default",
			"max-size-bytes": "Max size of all remaining cache items",
			"max-age":        "Max age of items",
			"all":            "List all entries",
		}, nil)
}

func boolYesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func (x *cmdSnapDownloadsCache) Execute(args []string) error {
	cacheDir := dirs.SnapDownloadCacheDir

	// same as overlord defaultCachePolicy
	policy := store.CachePolicy{
		// at most this many unreferenced items
		MaxItems: 5,
		// unreferenced items older than 30 days are removed
		MaxAge: 30 * 24 * time.Hour,
		// try to keep cache < 1GB
		MaxSizeBytes: 1 * 1024 * 1024 * 1024,
	}

	if x.Dir != "" {
		cacheDir = x.Dir
	}

	if x.MaxItems != nil {
		policy.MaxItems = int(*x.MaxItems)
	}

	if x.MaxAge != nil {
		policy.MaxAge = *x.MaxAge
	}

	if x.MaxSizeBytes != nil {
		policy.MaxSizeBytes = *x.MaxSizeBytes
	}

	cm := store.NewCacheManager(cacheDir, policy)

	stats, err := cm.Stats()
	if err != nil {
		return fmt.Errorf("cannot obtain cache stats: %w", err)
	}

	// TODO add ability to invoke cleanup?

	fmt.Fprintf(Stdout, "Cache location: %v\n", cacheDir)
	fmt.Fprintf(Stdout, "Max cache-unique items: %v\n", policy.MaxItems)
	fmt.Fprintf(Stdout, "Max total size of cache-unique items: %v\n", quantity.FormatAmount(policy.MaxSizeBytes, -1))
	fmt.Fprintf(Stdout, "Max age of cache-unique items: %v\n", policy.MaxAge)
	fmt.Fprintf(Stdout, "\n")
	fmt.Fprintf(Stdout, "Cache entries: %v\n", len(stats.Entries))
	fmt.Fprintf(Stdout, "Total size: %v\n", quantity.FormatAmount(stats.TotalSize, -1))
	removedSize := int64(0)
	candidatesSize := int64(0)
	if len(stats.Entries) > 0 {
		tw := tabwriter.NewWriter(Stdout, 2, 2, 1, ' ', 0)

		fmt.Fprintf(tw, "Name\tSize\tMod time\tCandidate\tWould remove\n")
		for _, entry := range stats.Entries {

			if !entry.Candidate && !x.All {
				continue
			}

			if entry.Candidate {
				candidatesSize += entry.Info.Size()
			}

			if entry.Remove {
				removedSize += entry.Info.Size()
			}

			fmt.Fprintf(tw, "%s\t%v\t%s\t%v\t%s\n",
				entry.Info.Name(),
				quantity.FormatAmount(uint64(entry.Info.Size()), -1),
				entry.Info.ModTime(),
				boolYesNo(entry.Candidate),
				boolYesNo(entry.Remove),
			)
		}
		tw.Flush()
	}

	fmt.Fprintf(Stdout, "Total removed size: %v\n", quantity.FormatAmount(uint64(removedSize), -1))
	fmt.Fprintf(Stdout, "Total candidates size: %v\n", quantity.FormatAmount(uint64(candidatesSize), -1))
	fmt.Fprintf(Stdout, "Remaining size: %v\n", quantity.FormatAmount((uint64(candidatesSize)-uint64(removedSize)), -1))

	return nil
}
