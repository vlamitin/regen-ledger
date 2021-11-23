//go:build norace
// +build norace

package testsuite_test

import (
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/vlamitin/regen-ledger/app/testsuite"
	"github.com/vlamitin/regen-ledger/types/testutil/network"
)

func TestNetwork(t *testing.T) {
	cfg := testsuite.DefaultConfig()

	suite.Run(t, network.NewIntegrationTestSuite(cfg))
}
