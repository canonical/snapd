package snappy

type Part interface {
	Name() string
	Tag() string

	CurrentVersion() string
	LatestVersion() string

	CurrentHash() string

	// true if DS is the current one
	Active() bool

	// true if DS is installed
	Installed() bool
}

type DataSource interface {

	Versions() []Part
	Update(parts []Part) bool
	Tags(part Part) []string
}
