package snappy

// Representation of a snappy part
type Part struct {
	Name string
	Tag  string

	CurrentVersion string
	LatestVersion  string

	CurrentHash string

	// true if part is the currently selected one
	Active bool

	// true if part is installed
	Installed bool
}

// A DataSource (DS)
// FIXME: we need a way for the caller to determine if _individual_
// methods are privileged so that the caller can quickly check if an
// operation would require root before actually calling it.
type DataSource interface {

	// returns a list of Part objects
	Versions() []Part

	// update the specified parts
	Update(parts []Part) (err error)

	Rollback(parts []Part) (err error)

	// return the available tags for a given part
	Tags(part Part) []string

	// compare two parts; return true if (a < b)
	Less(a, b Part) bool

	// true if manipulating the DS requires root.
	//
	// This is used to speed up command invocation: rather than
	// running a particular snappy command for all available
	// datasources and ultimately finding that the _last_ datasource
	// requires root, we check upfront to avoid running any
	// commands. This gives a better user experience since some
	// datasource commands are inherantly slow (they require network
	// access for example).
	//
	// This is rather crude since ideally we'd allow a DS's
	// methods to individually specify if they are privileged.
	Privileged() bool
}
