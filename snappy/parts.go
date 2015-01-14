package snappy

const (
	SNAPP_BASE_DIR = "/apps"
	SNAPP_OEM_DIR  = "/oem"
)

// Representation of a snappy part
type Part interface {

	// querry
	Name() string
	Version() string
	Description() string

	Hash() string
	IsActive() bool
	IsInstalled() bool

	InstalledSize() int
	DownloadSize() int

	// Action
	Install() error
	Uninstall() error
	Config(configuration []byte) error
}

type Repository interface {

	// query
	Description() string

	// action
	Search(terms string) ([]Part, error)
	GetUpdates() ([]Part, error)
	GetInstalled() ([]Part, error)
}

type MetaRepository struct {
	all []Repository
}

func NewMetaRepository() *MetaRepository {
	m := new(MetaRepository)
	m.all = []Repository{
		NewSystemImageRepository(),
		NewUbuntuStoreSnappRepository(),
		NewLocalSnappRepository(SNAPP_BASE_DIR),
		NewLocalSnappRepository(SNAPP_OEM_DIR)}

	return m
}

func (m *MetaRepository) GetInstalled() (parts []Part, err error) {
	for _, r := range m.all {
		installed, err := r.GetInstalled()
		if err != nil {
			return parts, err
		}
		// FIXME: python extend() anyone?
		for _, part := range installed {
			parts = append(parts, part)
		}
	}

	return parts, err
}

func (m *MetaRepository) GetUpdates() (parts []Part, err error) {
	for _, r := range m.all {
		updates, err := r.GetUpdates()
		if err != nil {
			return parts, err
		}
		// FIXME: python extend() anyone?
		for _, part := range updates {
			parts = append(parts, part)
		}
	}

	return parts, err
}

func (m *MetaRepository) Search(terms string) (parts []Part, err error) {
	for _, r := range m.all {
		results, err := r.Search(terms)
		if err != nil {
			return parts, err
		}
		// FIXME: python extend() anyone?
		for _, part := range results {
			parts = append(parts, part)
		}
	}

	return parts, err
}
