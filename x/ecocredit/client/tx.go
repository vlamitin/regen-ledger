package client

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"strings"

	sdkclient "github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	"github.com/vlamitin/regen-ledger/x/ecocredit"
)

// TxCmd returns a root CLI command handler for all x/ecocredit transaction commands.
func TxCmd(name string) *cobra.Command {
	cmd := &cobra.Command{
		SuggestionsMinimumDistance: 2,
		DisableFlagParsing:         true,

		Use:   name,
		Short: "Ecocredit module transactions",
		RunE:  sdkclient.ValidateCmd,
	}
	cmd.AddCommand(
		TxCreateClassCmd(),
		TxGenBatchJSONCmd(),
		TxCreateBatchCmd(),
		TxSendCmd(),
		TxRetireCmd(),
		TxCancelCmd(),
		TxUpdateClassMetadataCmd(),
		TxUpdateClassIssuersCmd(),
		TxUpdateClassAdminCmd(),
	)
	return cmd
}

func txflags(cmd *cobra.Command) *cobra.Command {
	flags.AddTxFlagsToCmd(cmd)
	cmd.MarkFlagRequired(flags.FlagFrom)
	return cmd
}

// TxCreateClassCmd returns a transaction command that creates a credit class.
func TxCreateClassCmd() *cobra.Command {
	return txflags(&cobra.Command{
		Use:   "create-class [issuer[,issuer]*] [credit type name] [metadata]",
		Short: "Creates a new credit class with transaction author (--from) as admin",
		Long: fmt.Sprintf(
			`Creates a new credit class with transaction author (--from) as admin.

The transaction author must have permission to create a new credit class by
being a member of the %s allowlist. This is a governance parameter, so can be
queried via the command line.

They must also pay the fee associated with creating a new credit class, defined
by the %s parameter, so should make sure they have enough funds to cover that.

Parameters:
  issuer:    	       comma separated (no spaces) list of issuer account addresses. Example: "addr1,addr2"
  credit type name:    the name of the credit class type (e.g. carbon, biodiversity, etc)
  metadata:  	       base64 encoded metadata - arbitrary data attached to the credit class info`,
			ecocredit.KeyAllowedClassCreators,
			ecocredit.KeyCreditClassFee,
		),
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := sdkclient.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			// Get the class admin from the --from flag
			admin := clientCtx.GetFromAddress()

			// Parse the comma-separated list of issuers
			issuers := strings.Split(args[0], ",")
			for i := range issuers {
				issuers[i] = strings.TrimSpace(issuers[i])
			}

			// Check credit type name is provided
			if args[1] == "" {
				return sdkerrors.ErrInvalidRequest.Wrap("credit type name is required")
			}
			creditTypeName := args[1]

			// Check that metadata is provided and decode it
			if args[2] == "" {
				return errors.New("base64_metadata is required")
			}
			b, err := base64.StdEncoding.DecodeString(args[2])
			if err != nil {
				return sdkerrors.ErrInvalidRequest.Wrap("metadata is malformed, proper base64 string is required")
			}

			msg := ecocredit.MsgCreateClass{
				Admin:          admin.String(),
				Issuers:        issuers,
				Metadata:       b,
				CreditTypeName: creditTypeName,
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), &msg)
		},
	})
}

const (
	FlagClassId         string = "class-id"
	FlagIssuances       string = "issuances"
	FlagStartDate       string = "start-date"
	FlagEndDate         string = "end-date"
	FlagProjectLocation string = "project-location"
	FlagMetadata        string = "metadata"
)

// TxGenBatchJSONCmd returns a transaction command that generates JSON to
// represent a new credit batch.
func TxGenBatchJSONCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gen-batch-json --class-id [class_id] --issuances [issuances] --start-date [start_date] --end-date [end_date] --project-location [project_location] --metadata [metadata]",
		Short: "Generates JSON to represent a new credit batch for use with create-batch command",
		Long: `Generates JSON to represent a new credit batch for use with create-batch command.

Required Flags:
  class_id:   id of the credit class
  issuances:  the amount of issuances to generate
  start-date: The beginning of the period during which this credit batch was
              quantified and verified. Format: yyyy-mm-dd.
  end-date:   The end of the period during which this credit batch was
              quantified and verified. Format: yyyy-mm-dd.
  project-location: the location of the credit batch (see documentation for proper project-location formats).
  metadata:   base64 encoded issuance metadata`,
		Args: cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			classId, err := cmd.Flags().GetString(FlagClassId)
			if err != nil {
				return err
			}

			templateIssuance := &ecocredit.MsgCreateBatch_BatchIssuance{
				Recipient:          "recipient-address",
				TradableAmount:     "tradable-amount",
				RetiredAmount:      "retired-amount",
				RetirementLocation: "retirement-location",
			}

			numIssuances, err := cmd.Flags().GetUint32(FlagIssuances)
			issuances := make([]*ecocredit.MsgCreateBatch_BatchIssuance, numIssuances)
			for i := range issuances {
				issuances[i] = templateIssuance
			}

			startDateStr, err := cmd.Flags().GetString(FlagStartDate)
			if err != nil {
				return err
			}
			startDate, err := ParseDate("start_date", startDateStr)
			if err != nil {
				return err
			}

			endDateStr, err := cmd.Flags().GetString(FlagEndDate)
			if err != nil {
				return err
			}
			endDate, err := ParseDate("end_date", endDateStr)
			if err != nil {
				return err
			}

			projectLocation, err := cmd.Flags().GetString(FlagProjectLocation)
			if err != nil {
				return err
			}

			metadataStr, err := cmd.Flags().GetString(FlagMetadata)
			if err != nil {
				return err
			}
			b, err := base64.StdEncoding.DecodeString(metadataStr)
			if err != nil {
				return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "metadata is malformed, proper base64 string is required")
			}

			msg := &ecocredit.MsgCreateBatch{
				ClassId:         classId,
				Issuance:        issuances,
				Metadata:        b,
				StartDate:       &startDate,
				EndDate:         &endDate,
				ProjectLocation: projectLocation,
			}

			// Marshal and output JSON of message
			ctx := sdkclient.GetClientContextFromCmd(cmd)
			msgJson, err := ctx.Codec.MarshalJSON(msg)
			if err != nil {
				return err
			}

			var formattedJson bytes.Buffer
			json.Indent(&formattedJson, msgJson, "", "    ")
			fmt.Println(formattedJson.String())

			return nil
		},
	}
	cmd.Flags().String(FlagClassId, "", "credit class")
	cmd.MarkFlagRequired(FlagClassId)
	cmd.Flags().Uint32(FlagIssuances, 0, "The number of template issuances to generate")
	cmd.MarkFlagRequired(FlagIssuances)
	cmd.Flags().String(FlagStartDate, "", "The beginning of the period during which this credit batch was quantified and verified. Format: yyyy-mm-dd.")
	cmd.MarkFlagRequired(FlagStartDate)
	cmd.Flags().String(FlagEndDate, "", "The end of the period during which this credit batch was quantified and verified. Format: yyyy-mm-dd.")
	cmd.MarkFlagRequired(FlagEndDate)
	cmd.Flags().String(FlagProjectLocation, "", "The location of the project that is backing the credits in this batch")
	cmd.MarkFlagRequired(FlagProjectLocation)
	cmd.Flags().String(FlagMetadata, "", "base64 encoded issuance metadata")
	return cmd
}

// TxCreateBatchCmd returns a transaction command that creates a credit batch.
func TxCreateBatchCmd() *cobra.Command {

	return txflags(&cobra.Command{
		Use:   "create-batch [msg-create-batch-json-file]",
		Short: "Issues a new credit batch",
		Long: fmt.Sprintf(`Issues a new credit batch.

Parameters:
  msg-create-batch-json-file: Path to a file containing a JSON object
                              representing MsgCreateBatch. The JSON has format:
                              {
                                "class_id"": "C01",
                                "issuance": [
                                  {
                                    "recipient":           "regen1elq7ys34gpkj3jyvqee0h6yk4h9wsfxmgqelsw",
                                    "tradable_amount":     "1000",
                                    "retired_amount":      "15",
                                    "retirement_location": "ST-UVW XY Z12",
                                  },
                                ],
                                "metadata":         "AQI=",
                                "start_date":       "1990-01-01",
                                "end_date":         "1995-10-31",
                                "project_location": "AB-CDE FG1 345",
                              }
                              `),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := sdkclient.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			contents, err := ioutil.ReadFile(args[0])
			if err != nil {
				return err
			}

			if err := checkDuplicateKey(json.NewDecoder(bytes.NewReader(contents)), nil); err != nil {
				return err
			}

			// Parse the JSON file representing the request
			msg, err := parseMsgCreateBatch(clientCtx, args[0])
			if err != nil {
				return sdkerrors.ErrInvalidRequest.Wrapf("parsing batch JSON:\n%s", err.Error())
			}

			// Get the batch issuer from the --from flag
			issuer := clientCtx.GetFromAddress()
			msg.Issuer = issuer.String()

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	})
}

// TxSendCmd returns a transaction command that sends credits from one account
// to another.
func TxSendCmd() *cobra.Command {
	return txflags(&cobra.Command{
		Use:   "send [recipient] [credits]",
		Short: "Sends credits from the transaction author (--from) to the recipient",
		Long: `Sends credits from the transaction author (--from) to the recipient.

Parameters:
  recipient: recipient address
  credits:   YAML encoded credit list. Note: numerical values must be written in strings.
             eg: '[{batch_denom: "C01-20210101-20220101-001", tradable_amount: "5", retired_amount: "0", retirement_location: "YY-ZZ 12345"}]'
             Note: "retirement_location" is only required when "retired_amount" is positive.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			var credits = []*ecocredit.MsgSend_SendCredits{}
			if err := yaml.Unmarshal([]byte(args[1]), &credits); err != nil {
				return err
			}
			clientCtx, err := sdkclient.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			msg := ecocredit.MsgSend{
				Sender:    clientCtx.GetFromAddress().String(),
				Recipient: args[0], Credits: credits,
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), &msg)
		},
	})
}

// TxRetireCmd returns a transaction command that retires credits.
func TxRetireCmd() *cobra.Command {
	return txflags(&cobra.Command{
		Use:   "retire [credits] [retirement_location]",
		Short: "Retires a specified amount of credits from the account of the transaction author (--from)",
		Long: `Retires a specified amount of credits from the account of the transaction author (--from)

Parameters:
  credits:             YAML encoded credit list. Note: numerical values must be written in strings.
                       eg: '[{batch_denom: "C01-20210101-20220101-001", amount: "5"}]'
  retirement_location: A string representing the location of the buyer or
                       beneficiary of retired credits. It has the form
                       <country-code>[-<region-code>[ <postal-code>]], where
                       country-code and region-code are taken from ISO 3166, and
                       postal-code being up to 64 alphanumeric characters.
                       eg: 'AA-BB 12345'`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			var credits = []*ecocredit.MsgRetire_RetireCredits{}
			if err := yaml.Unmarshal([]byte(args[0]), &credits); err != nil {
				return err
			}
			clientCtx, err := sdkclient.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			msg := ecocredit.MsgRetire{
				Holder:   clientCtx.GetFromAddress().String(),
				Credits:  credits,
				Location: args[1],
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), &msg)
		},
	})
}

// TxCancelCmd returns a transaction command that cancels credits.
func TxCancelCmd() *cobra.Command {
	return txflags(&cobra.Command{
		Use:   "cancel [credits]",
		Short: "Cancels a specified amount of credits from the account of the transaction author (--from)",
		Long: `Cancels a specified amount of credits from the account of the transaction author (--from)

Parameters:
  credits:  comma-separated list of credits in the form [<amount> <batch-denom>]
            eg: '10 C01-20200101-20210101-001, 0.1 C01-20200101-20210101-001'`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			credits, err := parseCancelCreditsList(args[0])
			if err != nil {
				return err
			}
			clientCtx, err := sdkclient.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			msg := ecocredit.MsgCancel{
				Holder:  clientCtx.GetFromAddress().String(),
				Credits: credits,
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), &msg)
		},
	})
}

func TxUpdateClassMetadataCmd() *cobra.Command {
	return txflags(&cobra.Command{
		Use:   "update-class-metadata [class-id] [metadata]",
		Short: "Updates the metadata for a specific credit class",
		Long: `Updates the metadata for a specific credit class. the '--from' flag must equal the credit class admin.

Parameters:
  class-id:  the class id that corresponds with the credit class you want to update
  metadata:  base64 encoded metadata`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {

			if args[0] == "" {
				return errors.New("class-id is required")
			}
			classID := args[0]

			// Check that metadata is provided and decode it
			if args[1] == "" {
				return errors.New("base64_metadata is required")
			}
			b, err := base64.StdEncoding.DecodeString(args[1])
			if err != nil {
				return sdkerrors.ErrInvalidRequest.Wrap("metadata is malformed, proper base64 string is required")
			}

			clientCtx, err := sdkclient.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			msg := ecocredit.MsgUpdateClassMetadata{
				Admin:    clientCtx.GetFromAddress().String(),
				ClassId:  classID,
				Metadata: b,
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), &msg)
		},
	})
}

func TxUpdateClassAdminCmd() *cobra.Command {
	return txflags(&cobra.Command{
		Use:   "update-class-admin [class-id] [admin]",
		Short: "Updates the admin for a specific credit class",
		Long: `Updates the admin for a specific credit class. the '--from' flag must equal the current credit class admin.
               WARNING: Updating the admin replaces the current admin. Be sure the address entered is correct.

Parameters:
  class-id:  the class id that corresponds with the credit class you want to update
  new-admin: the address to overwrite the current admin address`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {

			if args[0] == "" {
				return errors.New("class-id is required")
			}
			classID := args[0]

			// check for the address
			newAdmin := args[1]
			if newAdmin == "" {
				return errors.New("new admin address is required")
			}

			clientCtx, err := sdkclient.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			msg := ecocredit.MsgUpdateClassAdmin{
				Admin:    clientCtx.GetFromAddress().String(),
				ClassId:  classID,
				NewAdmin: newAdmin,
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), &msg)
		},
	})
}

func TxUpdateClassIssuersCmd() *cobra.Command {
	return txflags(&cobra.Command{
		Use:   "update-class-issuers [class-id] [issuers]",
		Short: "Update the list of issuers for a specific credit class",
		Long: `Update the list of issuers for a specific credit class. the '--from' flag must equal the current credit class admin.

Parameters:
  class-id:  the class id that corresponds with the credit class you want to update
  issuers:   the new list of issuers to replace the current issuers	
            eg: 'regen tx ecocredit update-class-issuers C01 addr1,addr2,addr3`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {

			if args[0] == "" {
				return errors.New("class-id is required")
			}
			classID := args[0]

			// Parse the comma-separated list of issuers
			issuers := strings.Split(args[1], ",")
			for i := range issuers {
				issuers[i] = strings.TrimSpace(issuers[i])
			}

			clientCtx, err := sdkclient.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			msg := ecocredit.MsgUpdateClassIssuers{
				Admin:   clientCtx.GetFromAddress().String(),
				ClassId: classID,
				Issuers: issuers,
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), &msg)
		},
	})
}
