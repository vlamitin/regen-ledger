// +build experimental

package testsuite

import (
	"testing"

	"github.com/stretchr/testify/suite"

	group "github.com/vlamitin/regen-ledger/x/group/client/testsuite"
)

func TestGroupIntegration(t *testing.T) {
	cfg := DefaultConfig()

	suite.Run(t, group.NewIntegrationTestSuite(cfg))
}
