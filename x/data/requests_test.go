package data

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cosmos/cosmos-sdk/testutil/testdata"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

func TestMsgAnchorDataRequest_GetSigners(t *testing.T) {
	_, _, addr := testdata.KeyTestPubAddr()

	msg := &MsgAnchorData{Sender: addr.String()}
	require.Equal(t, []sdk.AccAddress{addr}, msg.GetSigners())

	msg = &MsgAnchorData{Sender: ""}
	require.Panics(t, func() {
		msg.GetSigners()
	})
}

func TestMsgAnchorDataRequest_ValidateBasic(t *testing.T) {
	type fields struct {
		Sender string
		Hash   *ContentHash
	}
	tests := []struct {
		name    string
		fields  fields
		wantErr string
	}{
		{
			name: "good",
			fields: fields{
				Sender: "",
				Hash: &ContentHash{Sum: &ContentHash_Raw_{Raw: &ContentHash_Raw{
					Hash:            make([]byte, 32),
					DigestAlgorithm: DigestAlgorithm_DIGEST_ALGORITHM_BLAKE2B_256,
					MediaType:       MediaType_MEDIA_TYPE_UNSPECIFIED,
				},
				}},
			},
			wantErr: "",
		},
		{
			name: "bad",
			fields: fields{
				Sender: "",
				Hash: &ContentHash{Sum: &ContentHash_Raw_{Raw: &ContentHash_Raw{
					Hash:            make([]byte, 31),
					DigestAlgorithm: DigestAlgorithm_DIGEST_ALGORITHM_BLAKE2B_256,
					MediaType:       MediaType_MEDIA_TYPE_UNSPECIFIED,
				},
				}},
			},
			wantErr: "expected 32 bytes for DIGEST_ALGORITHM_BLAKE2B_256, got 31: invalid request",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &MsgAnchorData{
				Sender: tt.fields.Sender,
				Hash:   tt.fields.Hash,
			}
			err := m.ValidateBasic()
			if len(tt.wantErr) != 0 {
				require.EqualError(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestMsgSignDataRequest_GetSigners(t *testing.T) {
	_, _, addr := testdata.KeyTestPubAddr()
	_, _, addr2 := testdata.KeyTestPubAddr()

	msg := &MsgSignData{Signers: []string{addr.String(), addr2.String()}}
	require.Equal(t, []sdk.AccAddress{addr, addr2}, msg.GetSigners())

	msg = &MsgSignData{Signers: nil}
	require.Empty(t, msg.GetSigners())

	msg = &MsgSignData{Signers: []string{"abcd"}}
	require.Panics(t, func() {
		msg.GetSigners()
	})
}

func TestMsgSignDataRequest_ValidateBasic(t *testing.T) {
	type fields struct {
		Signers []string
		Hash    *ContentHash_Graph
	}
	tests := []struct {
		name    string
		fields  fields
		wantErr string
	}{
		{
			"good",
			fields{
				Signers: nil,
				Hash: &ContentHash_Graph{
					Hash:                      make([]byte, 32),
					DigestAlgorithm:           DigestAlgorithm_DIGEST_ALGORITHM_BLAKE2B_256,
					CanonicalizationAlgorithm: GraphCanonicalizationAlgorithm_GRAPH_CANONICALIZATION_ALGORITHM_URDNA2015,
					MerkleTree:                GraphMerkleTree_GRAPH_MERKLE_TREE_NONE_UNSPECIFIED,
				},
			},
			"",
		},
		{
			"bad",
			fields{
				Signers: nil,
				Hash: &ContentHash_Graph{
					Hash:                      make([]byte, 32),
					DigestAlgorithm:           DigestAlgorithm_DIGEST_ALGORITHM_BLAKE2B_256,
					CanonicalizationAlgorithm: GraphCanonicalizationAlgorithm_GRAPH_CANONICALIZATION_ALGORITHM_UNSPECIFIED,
					MerkleTree:                GraphMerkleTree_GRAPH_MERKLE_TREE_NONE_UNSPECIFIED,
				},
			},
			"invalid data.GraphCanonicalizationAlgorithm GRAPH_CANONICALIZATION_ALGORITHM_UNSPECIFIED: invalid request",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &MsgSignData{
				Signers: tt.fields.Signers,
				Hash:    tt.fields.Hash,
			}
			err := m.ValidateBasic()
			if len(tt.wantErr) != 0 {
				require.EqualError(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
