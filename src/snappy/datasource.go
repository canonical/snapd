package snappy

// Representation of a snappy part
type Part interface {
	Name() string
	Tag() string

	CurrentVersion() string
	LatestVersion() string

	CurrentHash() string

	// true if part is the currently selected one
	Active() bool

	// true if part is installed
	Installed() bool

	// compare two parts; return true if (a < b)
	Less(a, b Part) bool
}

// A DataSource (DS) 
type DataSource interface {

	// returns a list of Part objects
	Versions() []Part

	// update the specified parts
	Update(parts []Part) bool

	// return the available tags for a given part
	Tags(part Part) []string
}
