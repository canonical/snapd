package policy

func NewAppPolicy() appPolicy                { return appPolicy{} }
func NewBasePolicy(m string) *basePolicy     { return &basePolicy{modelBase: m} }
func NewGadgetPolicy(m string) *gadgetPolicy { return &gadgetPolicy{modelGadget: m} }
func NewKernelPolicy(m string) *kernelPolicy { return &kernelPolicy{modelKernel: m} }
func NewOSPolicy(m string) *osPolicy         { return &osPolicy{modelBase: m} }

var (
	ErrNoName       = errNoName
	ErrInUseForBoot = errInUseForBoot
	ErrRequired     = errRequired
	ErrIsModel      = errIsModel
)

func InUseByErr(snaps ...string) error {
	return inUseByErr(snaps)
}
