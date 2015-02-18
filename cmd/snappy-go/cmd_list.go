package main

import (
	"fmt"
	"io"
	"os"
	"text/tabwriter"
	"time"

	"launchpad.net/snappy/snappy"
)

type cmdList struct {
	Updates bool `short:"u" long:"updates" description:"Show available updates (requires network)"`
	Verbose bool `short:"v" long:"verbose" description:"Show channel information and expand all fields"`
}

const shortListHelp = `List active components installed on a snappy system`

const longListHelp = `Provides a list of all active components installed on a snappy system

If requested, the command will find out if there are updates for any of the components and indicate that by appending a * to the date. This will be slower as it requires a round trip to the app store on the network.

The developer information refers to non-mainline versions of a package (much like PPAs in deb-based Ubuntu). If the package is the primary version of that package in Ubuntu then the developer info is not shown. This allows one to identify packages which have custom, non-standard versions installed. As a special case, the “sideload” developer refers to packages installed manually on the system.

When a verbose listing is requested, information about the channel used is displayed; which is one of alpha, beta, rc or stable, and all fields are fully expanded too. In some cases, older (inactive) versions of snappy packages will be installed, these will be shown in the verbose output and the active version indicated with a * appended to the name of the component.`

func init() {
	var cmdListData cmdList
	cmd, err := parser.AddCommand("list", shortListHelp, longListHelp, &cmdListData)
	if err != nil {
		// panic here as something must be terribly wrong if there is an
		// error here
		panic(err)
	}

	cmd.Aliases = append(cmd.Aliases, "li")
}

func (x *cmdList) Execute(args []string) (err error) {
	return x.list()
}

func (x cmdList) list() error {
	installed, err := snappy.ListInstalled()
	if err != nil {
		return err
	}

	if x.Updates {
		updates, err := snappy.ListUpdates()
		if err != nil {
			return err
		}
		showUpdatesList(installed, updates, os.Stdout)
	} else if x.Verbose {
		showVerboseList(installed, os.Stdout)
	} else {
		showInstalledList(installed, os.Stdout)
	}

	return err
}

func formatDate(t time.Time) string {
	return fmt.Sprintf("%v-%02d-%v", t.Year(), int(t.Month()), t.Day())
}

func showInstalledList(installed []snappy.Part, o io.Writer) {
	w := tabwriter.NewWriter(o, 5, 3, 1, ' ', 0)

	fmt.Fprintln(w, "Name\tDate\tVersion\tSummary\t")
	for _, part := range installed {
		if part.IsActive() {
			fmt.Fprintln(w, fmt.Sprintf("%s\t%s\t%s\t%s\t", part.Name(), formatDate(part.Date()), part.Version(), part.Description()))
		}
	}
	w.Flush()

	showRebootMessage(installed, o)
}

func showVerboseList(installed []snappy.Part, o io.Writer) {
	w := tabwriter.NewWriter(o, 5, 3, 1, ' ', 0)

	fmt.Fprintln(w, "Name\tDate\tVersion\tSummary\t")
	for _, part := range installed {
		active := ""
		if part.IsActive() {
			active = "*"
		}
		fmt.Fprintln(w, fmt.Sprintf("%s%s\t%s\t%s\t%s\t", part.Name(), active, formatDate(part.Date()), part.Version(), part.Description()))
	}
	w.Flush()

	showRebootMessage(installed, o)
}

func showRebootMessage(installed []snappy.Part, o io.Writer) {
	// Initialise to handle systems without a provisioned "other"
	otherVersion := "0"
	currentVersion := "0"
	otherName := ""
	needsReboot := false

	for _, part := range installed {
		// FIXME: extend this later to look at more than just
		//        core - once we do that the logic here needs
		//        to be modified as the current code assumes
		//        there are only two version instaleld and
		//        there is only a single part that may requires
		//        a reboot
		if part.Type() != snappy.SnapTypeCore {
			continue
		}

		if part.NeedsReboot() {
			needsReboot = true
		}

		if part.IsActive() {
			currentVersion = part.Version()
		} else {
			otherVersion = part.Version()
			otherName = part.Name()
		}
	}

	if needsReboot {
		if snappy.VersionCompare(otherVersion, currentVersion) > 0 {
			fmt.Fprintln(o, fmt.Sprintf("Reboot to use the new %s.", otherName))
		} else {
			fmt.Fprintln(o, fmt.Sprintf("Reboot to use %s version %s.", otherName, otherVersion))
		}
	}
}

func showUpdatesList(installed []snappy.Part, updates []snappy.Part, o io.Writer) {
	// TODO tabwriter and output in general to adapt to the spec
	w := tabwriter.NewWriter(o, 5, 3, 1, ' ', 0)
	defer w.Flush()

	fmt.Fprintln(w, "Name\tDate\tVersion\t")
	for _, part := range installed {
		if !part.IsActive() {
			continue
		}
		hasUpdate := ""
		ver := part.Version()
		date := part.Date()
		update := snappy.FindSnapsByName(part.Name(), updates)
		if len(update) == 1 {
			hasUpdate = "*"
			ver = update[0].Version()
			date = update[0].Date()
		}
		fmt.Fprintln(w, fmt.Sprintf("%s%s\t%v\t%s\t", part.Name(), hasUpdate, formatDate(date), ver))
	}
}
