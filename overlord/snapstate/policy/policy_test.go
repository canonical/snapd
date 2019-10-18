package policy_test

import (
	"testing"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/policy"
	"github.com/snapcore/snapd/snap"
)

// tie testing into check (only needed once per package)
func TestSnapManager(t *testing.T) { check.TestingT(t) }

type policySuite struct{}

var _ = check.Suite(&policySuite{})

func (s *policySuite) TestFor(c *check.C) {
	for typ, pol := range map[snap.Type]snapstate.Policy{
		snap.TypeApp:    policy.NewAppPolicy(),
		snap.TypeBase:   policy.NewBasePolicy(""),
		snap.TypeGadget: policy.NewGadgetPolicy(""),
		snap.TypeKernel: policy.NewKernelPolicy(""),
		snap.TypeOS:     policy.NewOSPolicy(""),
		snap.TypeSnapd:  policy.NewSnapdPolicy(false),
	} {
		c.Check(policy.For(typ, &asserts.Model{}), check.FitsTypeOf, pol)
	}
}
