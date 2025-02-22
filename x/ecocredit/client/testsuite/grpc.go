package testsuite

import (
	"fmt"

	"github.com/cosmos/cosmos-sdk/types/rest"
	"github.com/vlamitin/regen-ledger/x/ecocredit"
)

func (s *IntegrationTestSuite) TestGetClasses() {
	val := s.network.Validators[0]

	testCases := []struct {
		name     string
		url      string
		expErr   bool
		expItems int
	}{
		{
			"invalid path",
			fmt.Sprintf("%s/regen/ecocredit/v1alpha1/class", val.APIAddress),
			true,
			0,
		},
		{
			"valid query",
			fmt.Sprintf("%s/regen/ecocredit/v1alpha1/classes", val.APIAddress),
			false,
			4,
		},
		{
			"valid query pagination",
			fmt.Sprintf("%s/regen/ecocredit/v1alpha1/classes?pagination.limit=2", val.APIAddress),
			false,
			2,
		},
	}

	require := s.Require()
	for _, tc := range testCases {
		tc := tc
		s.Run(tc.name, func() {
			resp, err := rest.GetRequest(tc.url)
			require.NoError(err)

			var classes ecocredit.QueryClassesResponse
			err = val.ClientCtx.Codec.UnmarshalJSON(resp, &classes)

			if tc.expErr {
				require.Error(err)
			} else {
				require.NoError(err)
				require.NotNil(classes.Classes)
				require.Len(classes.Classes, tc.expItems)
			}
		})
	}
}

func (s *IntegrationTestSuite) TestGetClass() {
	val := s.network.Validators[0]

	testCases := []struct {
		name    string
		url     string
		expErr  bool
		errMsg  string
		classID string
	}{
		{
			"invalid path",
			fmt.Sprintf("%s/regen/ecocredit/v1alpha1/class", val.APIAddress),
			true,
			"Not Implemented",
			"",
		},
		{
			"class not found",
			fmt.Sprintf("%s/regen/ecocredit/v1alpha1/classes/%s", val.APIAddress, "C999"),
			true,
			"not found",
			"",
		},
		{
			"valid class-id",
			fmt.Sprintf("%s/regen/ecocredit/v1alpha1/classes/%s", val.APIAddress, "C01"),
			false,
			"",
			"C01",
		},
	}

	require := s.Require()
	for _, tc := range testCases {
		tc := tc
		s.Run(tc.name, func() {
			resp, err := rest.GetRequest(tc.url)
			require.NoError(err)

			var class ecocredit.QueryClassInfoResponse
			err = val.ClientCtx.Codec.UnmarshalJSON(resp, &class)

			if tc.expErr {
				require.Error(err)
			} else {
				require.NoError(err)
				require.NotNil(class.Info)
				require.Contains(class.Info.ClassId, tc.classID)
			}
		})
	}
}

func (s *IntegrationTestSuite) TestGetBatches() {
	val := s.network.Validators[0]

	testCases := []struct {
		name       string
		url        string
		numBatches int
		expErr     bool
		errMsg     string
	}{
		{
			"invalid class-id",
			fmt.Sprintf("%s/regen/ecocredit/v1alpha1/classes/%s/batches", val.APIAddress, "abcd"),
			0,
			true,
			"class ID didn't match the format",
		},
		{
			"no batches found",
			fmt.Sprintf("%s/regen/ecocredit/v1alpha1/classes/%s/batches", val.APIAddress, "C100"),
			0,
			false,
			"",
		},
		{
			"valid request",
			fmt.Sprintf("%s/regen/ecocredit/v1alpha1/classes/%s/batches", val.APIAddress, "C01"),
			4,
			false,
			"",
		},
		{
			"valid request with pagination",
			fmt.Sprintf("%s/regen/ecocredit/v1alpha1/classes/%s/batches?pagination.limit=2", val.APIAddress, "C01"),
			2,
			false,
			"",
		},
	}

	require := s.Require()
	for _, tc := range testCases {
		tc := tc
		s.Run(tc.name, func() {
			resp, err := rest.GetRequest(tc.url)
			require.NoError(err)

			var batches ecocredit.QueryBatchesResponse
			err = val.ClientCtx.Codec.UnmarshalJSON(resp, &batches)

			if tc.expErr {
				require.Error(err)
				require.Contains(string(resp), tc.errMsg)
			} else {
				require.NoError(err)
				require.NotNil(batches.Batches)
				require.Len(batches.Batches, tc.numBatches)
			}
		})
	}
}

func (s *IntegrationTestSuite) TestGetBatch() {
	val := s.network.Validators[0]

	testCases := []struct {
		name    string
		url     string
		expErr  bool
		errMsg  string
		classID string
	}{
		{
			"invalid batch denom",
			fmt.Sprintf("%s/regen/ecocredit/v1alpha1/batches/%s", val.APIAddress, "C999"),
			true,
			"invalid denom",
			"",
		},
		{
			"no batches found",
			fmt.Sprintf("%s/regen/ecocredit/v1alpha1/batches/%s", val.APIAddress, "A00-00000000-00000000-000"),
			true,
			"not found",
			"",
		},
		{
			"valid request",
			fmt.Sprintf("%s/regen/ecocredit/v1alpha1/batches/%s", val.APIAddress, "C01-20210101-20210201-002"),
			false,
			"",
			"C01",
		},
	}

	require := s.Require()
	for _, tc := range testCases {
		tc := tc
		s.Run(tc.name, func() {
			resp, err := rest.GetRequest(tc.url)
			require.NoError(err)

			var batch ecocredit.QueryBatchInfoResponse
			err = val.ClientCtx.Codec.UnmarshalJSON(resp, &batch)

			if tc.expErr {
				require.Error(err)
				require.Contains(string(resp), tc.errMsg)
			} else {
				require.NoError(err)
				require.NotNil(batch.Info)
				require.Equal(batch.Info.ClassId, tc.classID)
			}
		})
	}
}

func (s *IntegrationTestSuite) TestCreditTypes() {
	require := s.Require()
	val := s.network.Validators[0]

	url := fmt.Sprintf("%s/regen/ecocredit/v1alpha1/credit-types", val.APIAddress)
	resp, err := rest.GetRequest(url)
	require.NoError(err)

	var creditTypes ecocredit.QueryCreditTypesResponse
	err = val.ClientCtx.Codec.UnmarshalJSON(resp, &creditTypes)

	require.NoError(err)
	require.Len(creditTypes.CreditTypes, 1)
	require.Equal(creditTypes.CreditTypes[0].Abbreviation, "C")
	require.Equal(creditTypes.CreditTypes[0].Name, "carbon")
}

func (s *IntegrationTestSuite) TestGetBalance() {
	val := s.network.Validators[0]

	testCases := []struct {
		name   string
		url    string
		expErr bool
		errMsg string
	}{
		{
			"invalid batch-denom",
			fmt.Sprintf("%s/regen/ecocredit/v1alpha1/batches/%s/balance/%s", val.APIAddress, "abcd", val.Address.String()),
			true,
			"invalid denom",
		},
		{
			"invalid account address",
			fmt.Sprintf("%s/regen/ecocredit/v1alpha1/batches/%s/balance/%s", val.APIAddress, "C01-20210101-20210201-001", "abcd"),
			true,
			"decoding bech32 failed",
		},
		{
			"valid request",
			fmt.Sprintf("%s/regen/ecocredit/v1alpha1/batches/%s/balance/%s", val.APIAddress, "C01-20210101-20210201-002", val.Address.String()),
			false,
			"",
		},
	}

	require := s.Require()
	for _, tc := range testCases {
		tc := tc
		s.Run(tc.name, func() {
			resp, err := rest.GetRequest(tc.url)
			require.NoError(err)

			var balance ecocredit.QueryBalanceResponse
			err = val.ClientCtx.Codec.UnmarshalJSON(resp, &balance)

			if tc.expErr {
				require.Error(err)
				require.Contains(string(resp), tc.errMsg)
			} else {
				require.NoError(err)
				require.NotNil(balance)
				require.Equal(balance.TradableAmount, "100")
				require.Equal(balance.RetiredAmount, "0.000001")
			}
		})
	}
}

func (s *IntegrationTestSuite) TestGetSupply() {
	val := s.network.Validators[0]

	testCases := []struct {
		name   string
		url    string
		expErr bool
		errMsg string
	}{
		{
			"invalid batch-denom",
			fmt.Sprintf("%s/regen/ecocredit/v1alpha1/batches/%s/supply", val.APIAddress, "abcd"),
			true,
			"invalid denom",
		},
		{
			"valid request",
			fmt.Sprintf("%s/regen/ecocredit/v1alpha1/batches/%s/supply", val.APIAddress, "C01-20210101-20210201-001"),
			false,
			"",
		},
	}

	require := s.Require()
	for _, tc := range testCases {
		tc := tc
		s.Run(tc.name, func() {
			resp, err := rest.GetRequest(tc.url)
			require.NoError(err)

			var supply ecocredit.QuerySupplyResponse
			err = val.ClientCtx.Codec.UnmarshalJSON(resp, &supply)

			if tc.expErr {
				require.Error(err)
				require.Contains(string(resp), tc.errMsg)
			} else {
				require.NoError(err)
				require.NotNil(supply)
				require.Equal(supply.RetiredSupply, "0.000001")
				require.Equal(supply.TradableSupply, "100")
			}
		})
	}
}

func (s *IntegrationTestSuite) TestGRPCQueryParams() {
	val := s.network.Validators[0]
	require := s.Require()

	resp, err := rest.GetRequest(fmt.Sprintf("%s/regen/ecocredit/v1alpha1/params", val.APIAddress))
	require.NoError(err)

	var params ecocredit.QueryParamsResponse
	require.NoError(val.ClientCtx.Codec.UnmarshalJSON(resp, &params))

	s.Require().Equal(ecocredit.DefaultParams(), *params.Params)
}
