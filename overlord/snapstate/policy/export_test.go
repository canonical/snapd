package policy

func NewAppPolicy() appPolicy                { return appPolicy{} }
func NewBasePolicy() basePolicy              { return basePolicy{} }
func NewGadgetPolicy() gadgetPolicy          { return gadgetPolicy{} }
func NewKernelPolicy(m string) *kernelPolicy { return &kernelPolicy{modelKernel: m} }
func NewOSPolicy(m string) *osPolicy         { return &osPolicy{modelBase: m} }
