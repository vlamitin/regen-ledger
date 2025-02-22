module github.com/vlamitin/regen-ledger/orm

go 1.17

require (
	github.com/cosmos/cosmos-sdk v0.44.2
	github.com/gogo/protobuf v1.3.3
	github.com/stretchr/testify v1.7.0
	github.com/tendermint/tm-db v0.6.4
	pgregory.net/rapid v0.4.7
)

replace github.com/cosmos/cosmos-sdk => github.com/regen-network/cosmos-sdk v0.44.2-regen-1

replace github.com/gogo/protobuf => github.com/regen-network/protobuf v1.3.3-alpha.regen.1
