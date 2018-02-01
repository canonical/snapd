package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/strutil/quantity"
)

type timeOpts struct {
	AbsTime bool `long:"absolute-times"`
	RelTime bool `long:"relative-times"`
}

var timeDescs = mixinDescs{
	"absolute-times": i18n.G("Always display absolute times (in YYYY-MM-DD HH:MM format)."),
	"relative-times": i18n.G("Always display relative times. If neither absolute nor relative times are requested, relative times are used for up to 30 days, and then absolute times in YYYY-MM-DD format."),
}

func fmtSize(size int64) string {
	return quantity.FormatAmount(uint64(size), -1)
}

func (opt timeOpts) fmtTime(t time.Time) string {
	if opt.AbsTime && !opt.RelTime {
		return t.Round(time.Minute).Format("2006-01-02 15:04")
	}
	ago := time.Since(t)
	if (opt.RelTime && !opt.AbsTime) || ago < 30*24*time.Hour {
		// TRANSLATORS: %s is a (separately translated) compact representation of a duration (e.g. 1h30m)
		return fmt.Sprintf(i18n.G("%s ago"), quantity.FormatDuration(ago.Seconds()))
	}

	return t.Round(24 * time.Hour).Format("2006-01-02")
}

type savedCmd struct {
	timeOpts
	Wide       bool       `long:"wide"`
	ID         snapshotID `long:"id"`
	Positional struct {
		Snaps []installedSnapName `positional-arg-name:"<snap>"`
	} `positional-args:"yes"`
}

var shortSavedHelp = i18n.G("List currently stored snapshots")
var longSavedHelp = i18n.G(`
The saved command lists the snapshots that have been created previously with the 'save' command.
`)

func (x *savedCmd) tabline(sg *client.SnapshotGroup, extraWidth int) string {
	if len(sg.Snapshots) == 0 {
		return fmt.Sprintf("%d\t-\t-\t-", sg.ID)
	}
	mint := sg.MinTime()
	snapNames := make([]string, 0, len(sg.Snapshots))
	var sz int64
	for _, sh := range sg.Snapshots {
		snapNames = append(snapNames, sh.Snap)
		sz += sh.Size
	}
	snaps := strings.Join(snapNames, ", ")
	if !x.Wide {
		width, _ := termSize()
		// size (5) + gutters (3+2+2; why is the first gutter 3?)
		width -= 12 + extraWidth
		if len(snaps) > width {
			snaps = snaps[:width-1] + "…"
		}
	}
	return fmt.Sprintf("%d\t%s\t%s\t%s", sg.ID, x.fmtTime(mint), snaps, fmtSize(sz))
}

func (x *savedCmd) Execute([]string) error {
	list, err := Client().Snapshots(uint64(x.ID), installedSnapNames(x.Positional.Snaps))
	if err != nil {
		return err
	}
	if len(list) == 0 {
		fmt.Fprintln(Stdout, "No snapshots found.")
		return nil
	}
	w := tabWriter()
	defer w.Flush()

	fmt.Fprintln(w, "ID\tDate\tSnaps\tSize")
	// the list is ordered by id
	minTimeWidth := len(x.fmtTime(list[0].MinTime()))
	maxIDwidth := len(strconv.FormatUint(list[len(list)-1].ID, 10))
	extraWidth := maxIDwidth + minTimeWidth

	for _, sg := range list {
		fmt.Fprintln(w, x.tabline(&sg, extraWidth))
	}

	return nil
}

type saveCmd struct {
	waitMixin
	timeOpts
	Homes      []string `long:"homes"`
	Positional struct {
		Snaps []installedSnapName `positional-arg-name:"<snap>"`
	} `positional-args:"yes"`
}

var shortSaveHelp = i18n.G("Save a snapshot of the current data")
var longSaveHelp = i18n.G(`
The save command creates a snapshot of the current data for the given snaps.
`)

func (x *saveCmd) Execute([]string) error {
	cli := Client()
	changeID, err := cli.SnapshotMany(installedSnapNames(x.Positional.Snaps), x.Homes)
	if err != nil {
		return err
	}
	chg, err := x.wait(cli, changeID)
	if err == noWait {
		return nil
	}
	if err != nil {
		return err
	}

	var shID snapshotID
	chg.Get("snapshot-id", &shID)
	y := &savedCmd{
		timeOpts: x.timeOpts,
		ID:       shID,
	}
	y.Positional.Snaps = x.Positional.Snaps
	return y.Execute(nil)
}

type loseCmd struct {
	waitMixin
	Positional struct {
		ID    snapshotID          `positional-arg-name:"<id>"`
		Snaps []installedSnapName `positional-arg-name:"<snap>"`
	} `positional-args:"yes" required:"yes"`
}

var shortLoseHelp = i18n.G("Delete a snapshot")
var longLoseHelp = i18n.G(`
The lose command deletes a snapshot.
`)

func (x *loseCmd) Execute([]string) error {
	cli := Client()
	snaps := installedSnapNames(x.Positional.Snaps)
	changeID, err := cli.LoseSnapshot(uint64(x.Positional.ID), snaps)
	if err != nil {
		return err
	}
	_, err = x.wait(cli, changeID)
	if err == noWait {
		return nil
	}
	if err != nil {
		return err
	}

	if len(snaps) > 0 {
		// TRANSLATORS: the %s is a comma-separated list of quoted snap names
		fmt.Fprintf(Stdout, i18n.G("Snapshot #%d of snaps %s is lost to the ages.\n"), x.Positional.ID, strutil.Quoted(snaps))
	} else {
		fmt.Fprintf(Stdout, i18n.G("Snapshot #%d is lost to the ages.\n"), x.Positional.ID)
	}
	return nil
}

type checkCmd struct {
	waitMixin
	Homes      string `long:"homes"`
	Positional struct {
		ID    snapshotID          `positional-arg-name:"<id>"`
		Snaps []installedSnapName `positional-arg-name:"<snap>"`
	} `positional-args:"yes" required:"yes"`
}

var shortCheckHelp = i18n.G("Check a snapshot")
var longCheckHelp = i18n.G(`
The check command checks a snapshot against its hashsums.
`)

func (x *checkCmd) Execute([]string) error {
	cli := Client()
	snaps := installedSnapNames(x.Positional.Snaps)
	homes := strings.Split(x.Homes, ",")
	changeID, err := cli.CheckSnapshot(uint64(x.Positional.ID), snaps, homes)
	if err != nil {
		return err
	}
	_, err = x.wait(cli, changeID)
	if err == noWait {
		return nil
	}
	if err != nil {
		return err
	}

	// TODO: also mention the home archives that were actually checked
	if len(snaps) > 0 {
		// TRANSLATORS: the %s is a comma-separated list of quoted snap names
		fmt.Fprintf(Stdout, i18n.G("Snapshot #%d is probably *fine*, at least for snaps %s.\n"),
			x.Positional.ID, strutil.Quoted(snaps))
	} else {
		fmt.Fprintf(Stdout, i18n.G("Snapshot #%d is probably *fine*.\n"), x.Positional.ID)
	}
	return nil
}

type restoreCmd struct {
	waitMixin
	Homes      string `long:"homes"`
	Positional struct {
		ID    snapshotID          `positional-arg-name:"<id>"`
		Snaps []installedSnapName `positional-arg-name:"<snap>"`
	} `positional-args:"yes" required:"yes"`
}

var shortRestoreHelp = i18n.G("Restore a snapshot")
var longRestoreHelp = i18n.G(`
The restore command restores a snapshot.
`)

func (x *restoreCmd) Execute([]string) error {
	cli := Client()
	snaps := installedSnapNames(x.Positional.Snaps)
	homes := strings.Split(x.Homes, ",")
	changeID, err := cli.RestoreSnapshot(uint64(x.Positional.ID), snaps, homes)
	if err != nil {
		return err
	}
	_, err = x.wait(cli, changeID)
	if err == noWait {
		return nil
	}
	if err != nil {
		return err
	}

	// TODO: also mention the home archives that were actually restoreed
	if len(snaps) > 0 {
		// TRANSLATORS: the %s is a comma-separated list of quoted snap names
		fmt.Fprintf(Stdout, i18n.G("Snapshot #%d of %s has been restored.\n"),
			x.Positional.ID, strutil.Quoted(snaps))
	} else {
		fmt.Fprintf(Stdout, i18n.G("Snapshot #%d has been restored.\n"), x.Positional.ID)
	}
	return nil
}

func init() {
	addCommand("saved",
		shortSavedHelp,
		longSavedHelp,
		func() flags.Commander {
			return &savedCmd{}
		},
		timeDescs.also(map[string]string{
			"wide": i18n.G("Ignore terminal width and print all available information"),
			"id":   i18n.G("Only list this snapshot."),
		}),
		nil)

	addCommand("save",
		shortSaveHelp,
		longSaveHelp,
		func() flags.Commander {
			return &saveCmd{}
		}, nil, nil)

	addCommand("restore",
		shortRestoreHelp,
		longRestoreHelp,
		func() flags.Commander {
			return &restoreCmd{}
		}, nil, nil)

	addCommand("lose",
		shortLoseHelp,
		longLoseHelp,
		func() flags.Commander {
			return &loseCmd{}
		}, nil, nil)

	addCommand("check-snapshot",
		shortCheckHelp,
		longCheckHelp,
		func() flags.Commander {
			return &checkCmd{}
		}, nil, nil)
}
