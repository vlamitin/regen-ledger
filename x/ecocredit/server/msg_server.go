package server

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/vlamitin/regen-ledger/orm"
	"github.com/vlamitin/regen-ledger/types"
	"github.com/vlamitin/regen-ledger/types/math"
	"github.com/vlamitin/regen-ledger/x/ecocredit"
)

// TODO: Revisit this once we have proper gas fee framework.
// Tracking issues https://github.com/cosmos/cosmos-sdk/issues/9054, https://github.com/cosmos/cosmos-sdk/discussions/9072
const gasCostPerIteration = uint64(10)

// CreateClass creates a new class of ecocredit
//
// The admin is charged a fee for creating the class. This is controlled by
// the global parameter CreditClassFee, which can be updated through the
// governance process.
func (s serverImpl) CreateClass(goCtx context.Context, req *ecocredit.MsgCreateClass) (*ecocredit.MsgCreateClassResponse, error) {
	ctx := types.UnwrapSDKContext(goCtx)

	// Charge the admin a fee to create the credit class
	adminAddress, err := sdk.AccAddressFromBech32(req.Admin)
	if err != nil {
		return nil, err
	}

	var params ecocredit.Params
	s.paramSpace.GetParamSet(ctx.Context, &params)
	if params.AllowlistEnabled && !s.isCreatorAllowListed(ctx, params.AllowedClassCreators, adminAddress) {
		return nil, sdkerrors.ErrUnauthorized.Wrapf("%s is not allowed to create credit classes", adminAddress.String())
	}

	err = s.chargeCreditClassFee(ctx.Context, adminAddress)
	if err != nil {
		return nil, err
	}

	creditType, err := s.getCreditType(ctx.Context, req.CreditTypeName)
	if err != nil {
		return nil, err
	}

	classSeqNo, err := s.getCreditTypeSeqNextVal(ctx.Context, creditType)
	if err != nil {
		return nil, err
	}

	classID := ecocredit.FormatClassID(creditType, classSeqNo)

	err = s.classInfoTable.Create(ctx, &ecocredit.ClassInfo{
		ClassId:    classID,
		Admin:      req.Admin,
		Issuers:    req.Issuers,
		Metadata:   req.Metadata,
		CreditType: &creditType,
	})
	if err != nil {
		return nil, err
	}

	err = ctx.EventManager().EmitTypedEvent(&ecocredit.EventCreateClass{
		ClassId: classID,
		Admin:   req.Admin,
	})
	if err != nil {
		return nil, err
	}

	return &ecocredit.MsgCreateClassResponse{ClassId: classID}, nil
}

// CreateBatch creates a new batch of credits.
// Credits in the batch must not have more decimal places than the credit type's specified precision.
func (s serverImpl) CreateBatch(goCtx context.Context, req *ecocredit.MsgCreateBatch) (*ecocredit.MsgCreateBatchResponse, error) {
	ctx := types.UnwrapSDKContext(goCtx)
	classID := req.ClassId
	classInfo, err := s.getClassInfo(ctx, classID)
	if err != nil {
		return nil, err
	}

	if err = classInfo.AssertClassIssuer(req.Issuer); err != nil {
		return nil, err
	}

	maxDecimalPlaces := classInfo.CreditType.Precision
	batchSeqNo, err := s.nextBatchInClass(ctx, classInfo)
	if err != nil {
		return nil, sdkerrors.ErrInvalidRequest.Wrap(err.Error())
	}

	batchDenomStr, err := ecocredit.FormatDenom(classID, batchSeqNo, req.StartDate, req.EndDate)
	if err != nil {
		return nil, sdkerrors.ErrInvalidRequest.Wrap(err.Error())
	}

	batchDenom := batchDenomT(batchDenomStr)
	tradableSupply := math.NewDecFromInt64(0)
	retiredSupply := math.NewDecFromInt64(0)

	store := ctx.KVStore(s.storeKey)

	for _, issuance := range req.Issuance {
		var err error
		tradable, retired := math.NewDecFromInt64(0), math.NewDecFromInt64(0)

		if issuance.TradableAmount != "" {
			tradable, err = math.NewNonNegativeDecFromString(issuance.TradableAmount)
			if err != nil {
				return nil, err
			}

			decPlaces := tradable.NumDecimalPlaces()
			if decPlaces > maxDecimalPlaces {
				return nil, sdkerrors.ErrInvalidRequest.Wrapf("tradable amount exceeds precision for credit type: "+
					"is %v, should be < %v", decPlaces, maxDecimalPlaces)
			}
		}

		if issuance.RetiredAmount != "" {
			retired, err = math.NewNonNegativeDecFromString(issuance.RetiredAmount)
			if err != nil {
				return nil, err
			}

			decPlaces := retired.NumDecimalPlaces()
			if decPlaces > maxDecimalPlaces {
				return nil, sdkerrors.ErrInvalidRequest.Wrapf("retired amount does not conform to credit type "+
					"precision: %v should be %v", decPlaces, maxDecimalPlaces)
			}
		}

		recipient := issuance.Recipient
		recipientAddr, err := sdk.AccAddressFromBech32(recipient)
		if err != nil {
			return nil, err
		}

		if !tradable.IsZero() {
			tradableSupply, err = tradableSupply.Add(tradable)
			if err != nil {
				return nil, err
			}

			err = addAndSetDecimal(store, TradableBalanceKey(recipientAddr, batchDenom), tradable)
			if err != nil {
				return nil, err
			}
		}

		if !retired.IsZero() {
			retiredSupply, err = retiredSupply.Add(retired)
			if err != nil {
				return nil, err
			}

			err = retire(ctx, store, recipientAddr, batchDenom, retired, issuance.RetirementLocation)
			if err != nil {
				return nil, err
			}
		}

		err = ctx.EventManager().EmitTypedEvent(&ecocredit.EventReceive{
			Recipient:      recipient,
			BatchDenom:     string(batchDenom),
			RetiredAmount:  tradable.String(),
			TradableAmount: retired.String(),
		})
		if err != nil {
			return nil, err
		}

		ctx.GasMeter().ConsumeGas(gasCostPerIteration, "batch issuance")
	}

	setDecimal(store, TradableSupplyKey(batchDenom), tradableSupply)
	setDecimal(store, RetiredSupplyKey(batchDenom), retiredSupply)

	totalSupply, err := tradableSupply.Add(retiredSupply)
	if err != nil {
		return nil, err
	}
	totalSupplyStr := totalSupply.String()

	amountCancelledStr := math.NewDecFromInt64(0).String()

	err = s.batchInfoTable.Create(ctx, &ecocredit.BatchInfo{
		ClassId:         classID,
		BatchDenom:      string(batchDenom),
		Issuer:          req.Issuer,
		TotalAmount:     totalSupplyStr,
		Metadata:        req.Metadata,
		AmountCancelled: amountCancelledStr,
		StartDate:       req.StartDate,
		EndDate:         req.EndDate,
		ProjectLocation: req.ProjectLocation,
	})
	if err != nil {
		return nil, err
	}

	err = ctx.EventManager().EmitTypedEvent(&ecocredit.EventCreateBatch{
		ClassId:         classID,
		BatchDenom:      string(batchDenom),
		Issuer:          req.Issuer,
		TotalAmount:     totalSupplyStr,
		StartDate:       req.StartDate.Format("2006-01-02"),
		EndDate:         req.EndDate.Format("2006-01-02"),
		ProjectLocation: req.ProjectLocation,
	})
	if err != nil {
		return nil, err
	}

	return &ecocredit.MsgCreateBatchResponse{BatchDenom: string(batchDenom)}, nil
}

// Send sends credits to a recipient.
// Send also retires credits if the amount to retire is specified in the request.
func (s serverImpl) Send(goCtx context.Context, req *ecocredit.MsgSend) (*ecocredit.MsgSendResponse, error) {
	ctx := types.UnwrapSDKContext(goCtx)
	store := ctx.KVStore(s.storeKey)
	sender := req.Sender
	recipient := req.Recipient

	senderAddr, err := sdk.AccAddressFromBech32(sender)
	if err != nil {
		return nil, err
	}

	recipientAddr, err := sdk.AccAddressFromBech32(recipient)
	if err != nil {
		return nil, err
	}

	for _, credit := range req.Credits {
		err := s.sendEcocredits(ctx, credit, store, senderAddr, recipientAddr)
		if err != nil {
			return nil, err
		}

		ctx.GasMeter().ConsumeGas(gasCostPerIteration, "send ecocredits")
	}

	return &ecocredit.MsgSendResponse{}, nil
}

// Retire credits to the specified location.
// WARNING: retiring credits is permanent. Retired credits cannot be un-retired.
func (s serverImpl) Retire(goCtx context.Context, req *ecocredit.MsgRetire) (*ecocredit.MsgRetireResponse, error) {
	ctx := types.UnwrapSDKContext(goCtx)
	store := ctx.KVStore(s.storeKey)
	holderAddr, err := sdk.AccAddressFromBech32(req.Holder)
	if err != nil {
		return nil, err
	}

	for _, credit := range req.Credits {
		denom := batchDenomT(credit.BatchDenom)
		if !s.batchInfoTable.Has(ctx, orm.RowID(denom)) {
			return nil, sdkerrors.ErrInvalidRequest.Wrapf("%s is not a valid credit batch denom", denom)
		}

		maxDecimalPlaces, err := s.getBatchPrecision(ctx, denom)
		if err != nil {
			return nil, err
		}

		toRetire, err := math.NewPositiveFixedDecFromString(credit.Amount, maxDecimalPlaces)
		if err != nil {
			return nil, err
		}

		err = subtractTradableBalanceAndSupply(store, holderAddr, denom, toRetire)
		if err != nil {
			return nil, err
		}

		//  Add retired balance
		err = retire(ctx, store, holderAddr, denom, toRetire, req.Location)
		if err != nil {
			return nil, err
		}

		//  Add retired supply
		err = addAndSetDecimal(store, RetiredSupplyKey(denom), toRetire)
		if err != nil {
			return nil, err
		}

		ctx.GasMeter().ConsumeGas(gasCostPerIteration, "retire ecocredits")
	}

	return &ecocredit.MsgRetireResponse{}, nil
}

// Cancel credits, removing them from the supply and balance of the holder
func (s serverImpl) Cancel(goCtx context.Context, req *ecocredit.MsgCancel) (*ecocredit.MsgCancelResponse, error) {
	ctx := types.UnwrapSDKContext(goCtx)
	store := ctx.KVStore(s.storeKey)
	holderAddr, err := sdk.AccAddressFromBech32(req.Holder)
	if err != nil {
		return nil, err
	}

	for _, credit := range req.Credits {

		// Check that the batch that were trying to cancel credits from
		// exists
		denom := batchDenomT(credit.BatchDenom)
		if !s.batchInfoTable.Has(ctx, orm.RowID(denom)) {
			return nil, sdkerrors.ErrInvalidRequest.Wrapf("%s is not a valid credit batch denom", denom)
		}

		// Remove the credits from the total_amount in the batch and add
		// them to amount_cancelled
		var batchInfo ecocredit.BatchInfo
		err := s.batchInfoTable.GetOne(ctx, orm.RowID(denom), &batchInfo)
		if err != nil {
			return nil, err
		}

		classInfo, err := s.getClassInfo(ctx, batchInfo.ClassId)
		if err != nil {
			return nil, err
		}

		maxDecimalPlaces := classInfo.CreditType.Precision

		// Parse the amount of credits to cancel, checking it conforms
		// to the precision
		toCancel, err := math.NewPositiveFixedDecFromString(credit.Amount, maxDecimalPlaces)
		if err != nil {
			return nil, err
		}

		// Remove the credits from the balance of the holder and the
		// overall supply
		err = subtractTradableBalanceAndSupply(store, holderAddr, denom, toCancel)
		if err != nil {
			return nil, err
		}

		totalAmount, err := math.NewPositiveFixedDecFromString(batchInfo.TotalAmount, maxDecimalPlaces)
		if err != nil {
			return nil, err
		}

		totalAmount, err = math.SafeSubBalance(totalAmount, toCancel)
		if err != nil {
			return nil, err
		}
		batchInfo.TotalAmount = totalAmount.String()

		amountCancelled, err := math.NewNonNegativeFixedDecFromString(batchInfo.AmountCancelled, maxDecimalPlaces)
		if err != nil {
			return nil, err
		}

		amountCancelled, err = amountCancelled.Add(toCancel)
		if err != nil {
			return nil, err
		}
		batchInfo.AmountCancelled = amountCancelled.String()

		if err = s.batchInfoTable.Update(ctx, &batchInfo); err != nil {
			return nil, err
		}

		// Emit the cancellation event
		err = ctx.EventManager().EmitTypedEvent(&ecocredit.EventCancel{
			Canceller:  req.Holder,
			BatchDenom: string(denom),
			Amount:     toCancel.String(),
		})
		if err != nil {
			return nil, err
		}

		ctx.GasMeter().ConsumeGas(gasCostPerIteration, "cancel ecocredits")
	}

	return &ecocredit.MsgCancelResponse{}, nil
}

func (s serverImpl) UpdateClassAdmin(goCtx context.Context, req *ecocredit.MsgUpdateClassAdmin) (*ecocredit.MsgUpdateClassAdminResponse, error) {
	ctx := types.UnwrapSDKContext(goCtx)
	cInfo, err := s.getClassInfo(ctx, req.ClassId)
	if err != nil {
		return nil, err
	}

	if cInfo.Admin != req.Admin {
		return nil, sdkerrors.ErrUnauthorized.Wrapf("you are not the administrator of this class")
	}

	cInfo.Admin = req.NewAdmin
	err = s.classInfoTable.Update(ctx, cInfo)

	return &ecocredit.MsgUpdateClassAdminResponse{}, err
}

func (s serverImpl) UpdateClassIssuers(goCtx context.Context, req *ecocredit.MsgUpdateClassIssuers) (*ecocredit.MsgUpdateClassIssuersResponse, error) {
	ctx := types.UnwrapSDKContext(goCtx)
	cInfo, err := s.getClassInfo(ctx, req.ClassId)
	if err != nil {
		return nil, err
	}

	if cInfo.Admin != req.Admin {
		return nil, sdkerrors.ErrUnauthorized.Wrapf("you are not the administrator of this class")
	}

	cInfo.Issuers = req.Issuers
	err = s.classInfoTable.Update(ctx, cInfo)

	return &ecocredit.MsgUpdateClassIssuersResponse{}, err
}

func (s serverImpl) UpdateClassMetadata(goCtx context.Context, req *ecocredit.MsgUpdateClassMetadata) (*ecocredit.MsgUpdateClassMetadataResponse, error) {
	ctx := types.UnwrapSDKContext(goCtx)
	cInfo, err := s.getClassInfo(ctx, req.ClassId)
	if err != nil {
		return nil, err
	}

	if cInfo.Admin != req.Admin {
		return nil, sdkerrors.ErrUnauthorized.Wrapf("you are not the administrator of this class")
	}

	cInfo.Metadata = req.Metadata
	err = s.classInfoTable.Update(ctx, cInfo)

	return &ecocredit.MsgUpdateClassMetadataResponse{}, err
}

// nextBatchInClass gets the sequence number for the next batch in the credit
// class and updates the class info with the new batch number
func (s serverImpl) nextBatchInClass(ctx types.Context, classInfo *ecocredit.ClassInfo) (uint64, error) {
	// Get the next value
	nextVal := classInfo.NumBatches + 1

	// Update the ClassInfo
	classInfo.NumBatches = nextVal
	err := s.classInfoTable.Update(ctx, classInfo)
	if err != nil {
		return 0, err
	}

	return nextVal, nil
}

func retire(ctx types.Context, store sdk.KVStore, recipient sdk.AccAddress, batchDenom batchDenomT, retired math.Dec, location string) error {
	err := addAndSetDecimal(store, RetiredBalanceKey(recipient, batchDenom), retired)
	if err != nil {
		return err
	}

	return ctx.EventManager().EmitTypedEvent(&ecocredit.EventRetire{
		Retirer:    recipient.String(),
		BatchDenom: string(batchDenom),
		Amount:     retired.String(),
		Location:   location,
	})
}

// subtracts `amount` from the tradable balance and tradable supply
func subtractTradableBalanceAndSupply(store sdk.KVStore, holder sdk.AccAddress, batchDenom batchDenomT, amount math.Dec) error {
	// subtract tradable balance
	err := subAndSetDecimal(store, TradableBalanceKey(holder, batchDenom), amount)
	if err != nil {
		return err
	}

	// subtract tradable supply
	err = subAndSetDecimal(store, TradableSupplyKey(batchDenom), amount)
	if err != nil {
		return err
	}

	return nil
}

// gets the precision of the credit type associated with the batch
func (s serverImpl) getBatchPrecision(ctx types.Context, denom batchDenomT) (uint32, error) {
	var batchInfo ecocredit.BatchInfo
	err := s.batchInfoTable.GetOne(ctx, orm.RowID(denom), &batchInfo)
	if err != nil {
		return 0, err
	}

	classInfo, err := s.getClassInfo(ctx, batchInfo.ClassId)
	if err != nil {
		return 0, err
	}

	return classInfo.CreditType.Precision, nil
}

// Checks if the given address is in the allowlist of credit class creators
func (s serverImpl) isCreatorAllowListed(ctx types.Context, allowlist []string, designer sdk.Address) bool {
	for _, addr := range allowlist {
		ctx.GasMeter().ConsumeGas(gasCostPerIteration, "credit class creators allowlist")
		allowListedAddr, _ := sdk.AccAddressFromBech32(addr)
		if designer.Equals(allowListedAddr) {
			return true
		}
	}
	return false
}

// Sell creates new sell orders for credits
func (s serverImpl) Sell(goCtx context.Context, req *ecocredit.MsgSell) (*ecocredit.MsgSellResponse, error) {
	ctx := types.UnwrapSDKContext(goCtx)
	owner := req.Owner
	store := ctx.KVStore(s.storeKey)

	ownerAddr, err := sdk.AccAddressFromBech32(owner)
	if err != nil {
		return nil, err
	}

	sellOrderIds := make([]uint64, len(req.Orders))

	for i, order := range req.Orders {

		err = verifyCreditBalance(store, ownerAddr, order.BatchDenom, order.Quantity)
		if err != nil {
			return nil, err
		}

		// TODO: Verify that AskPrice.Denom is in AllowAskDenom #624

		orderID := s.sellOrderTable.Sequence().PeekNextVal(ctx)

		sellOrderIds[i] = orderID

		_, err = s.sellOrderTable.Create(ctx, &ecocredit.SellOrder{
			OrderId:           orderID,
			BatchDenom:        order.BatchDenom,
			Quantity:          order.Quantity,
			AskPrice:          order.AskPrice,
			DisableAutoRetire: order.DisableAutoRetire,
		})
		if err != nil {
			return nil, err
		}

		err = ctx.EventManager().EmitTypedEvent(&ecocredit.EventSell{
			OrderId:           orderID,
			BatchDenom:        order.BatchDenom,
			Quantity:          order.Quantity,
			AskPrice:          order.AskPrice,
			DisableAutoRetire: order.DisableAutoRetire,
		})
		if err != nil {
			return nil, err
		}

		ctx.GasMeter().ConsumeGas(gasCostPerIteration, "create sell order")
	}

	return &ecocredit.MsgSellResponse{SellOrderIds: sellOrderIds}, nil
}

// UpdateSellOrders updates existing sell orders for credits
func (s serverImpl) UpdateSellOrders(goCtx context.Context, req *ecocredit.MsgUpdateSellOrders) (*ecocredit.MsgUpdateSellOrdersResponse, error) {
	ctx := types.UnwrapSDKContext(goCtx)
	owner := req.Owner
	store := ctx.KVStore(s.storeKey)

	ownerAddr, err := sdk.AccAddressFromBech32(owner)
	if err != nil {
		return nil, err
	}

	for _, update := range req.Updates {

		sellOrder, err := s.getSellOrder(ctx, update.SellOrderId)
		if err != nil {
			return nil, err
		}

		// TODO: Verify that NewAskPrice.Denom is in AllowAskDenom #624

		err = verifyCreditBalance(store, ownerAddr, sellOrder.BatchDenom, update.NewQuantity)
		if err != nil {
			return nil, err
		}

		sellOrder.Quantity = update.NewQuantity
		sellOrder.AskPrice = update.NewAskPrice
		sellOrder.DisableAutoRetire = update.DisableAutoRetire

		err = s.sellOrderTable.Update(ctx, sellOrder.OrderId, sellOrder)
		if err != nil {
			return nil, err
		}

		err = ctx.EventManager().EmitTypedEvent(&ecocredit.EventUpdateSellOrder{
			Owner:             owner,
			SellOrderId:       sellOrder.OrderId,
			BatchDenom:        sellOrder.BatchDenom,
			NewQuantity:       sellOrder.Quantity,
			NewAskPrice:       sellOrder.AskPrice,
			DisableAutoRetire: sellOrder.DisableAutoRetire,
		})
		if err != nil {
			return nil, err
		}

		ctx.GasMeter().ConsumeGas(gasCostPerIteration, "update sell order")
	}

	return &ecocredit.MsgUpdateSellOrdersResponse{}, nil
}

// Buy creates new buy orders for credits
func (s serverImpl) Buy(goCtx context.Context, req *ecocredit.MsgBuy) (*ecocredit.MsgBuyResponse, error) {
	ctx := types.UnwrapSDKContext(goCtx)
	sdkCtx := sdk.UnwrapSDKContext(goCtx)
	store := ctx.KVStore(s.storeKey)
	buyer := req.Buyer

	buyerAddr, err := sdk.AccAddressFromBech32(buyer)
	if err != nil {
		return nil, err
	}

	buyOrderIds := make([]uint64, len(req.Orders))

	for i, order := range req.Orders {

		balances := s.bankKeeper.SpendableCoins(sdkCtx, buyerAddr)
		bidPrice := order.BidPrice
		balanceAmount := balances.AmountOf(bidPrice.Denom)

		// TODO: Verify that bidPrice.Denom is in AllowAskDenom #624

		// get decimal amount of credits desired for purchase
		creditsDesired, err := math.NewPositiveDecFromString(order.Quantity)
		if err != nil {
			return nil, err
		}

		// calculate the amount of coin to send for purchase
		coinToSend, err := getCoinNeeded(creditsDesired, bidPrice)
		if err != nil {
			return nil, err
		}

		// verify buyer has sufficient balance in coin
		if balanceAmount.LT(coinToSend.Amount) {
			return nil, ecocredit.ErrInsufficientFunds.Wrapf("insufficient balance: got %s, needed at least: %s", balanceAmount.String(), coinToSend.Amount.String())
		}

		switch order.Selection.Sum.(type) {
		case *ecocredit.MsgBuy_Order_Selection_SellOrderId:

			sellOrderId := order.Selection.GetSellOrderId()
			sellOrder, err := s.getSellOrder(ctx, sellOrderId)
			if err != nil {
				return nil, err
			}

			sellerAddr, err := sdk.AccAddressFromBech32(sellOrder.Owner)
			if err != nil {
				return nil, err
			}

			// verify bid price and ask price denoms match
			if bidPrice.Denom != sellOrder.AskPrice.Denom {
				return nil, sdkerrors.ErrInvalidRequest.Wrapf("bid price denom does not match ask price denom: got %s, expected: %s", bidPrice.Denom, sellOrder.AskPrice.Denom)
			}

			// verify bid price is greater than or equal to ask price
			if bidPrice.Amount.LT(sellOrder.AskPrice.Amount) {
				return nil, sdkerrors.ErrInvalidRequest.Wrapf("bid price too low: got %s, needed at least: %s", bidPrice.String(), sellOrder.AskPrice.String())
			}

			// verify seller has sufficient balance in credits
			err = verifyCreditBalance(store, sellerAddr, sellOrder.BatchDenom, sellOrder.Quantity)
			if err != nil {
				return nil, ecocredit.ErrInvalidSellOrder.Wrap(err.Error())
			}

			// get decimal amount of credits available for purchase
			creditsAvailable, err := math.NewDecFromString(sellOrder.Quantity)
			if err != nil {
				return nil, ecocredit.ErrInvalidSellOrder.Wrap(err.Error())
			}

			creditsToReceive := creditsDesired

			// check if credits desired is more than credits available
			if creditsDesired.Cmp(creditsAvailable) == 1 {

				// error if partial fill disabled
				if order.DisablePartialFill {
					return nil, ecocredit.ErrInsufficientFunds.Wrap("sell order does not have sufficient credits to fill the buy order")
				}

				creditsToReceive = creditsAvailable

				// recalculate coinToSend if creditsToReceive is not creditsDesired
				coinToSend, err = getCoinNeeded(creditsToReceive, bidPrice)
				if err != nil {
					return nil, err
				}
			}

			// send coin to the seller account
			err = s.bankKeeper.SendCoins(sdkCtx, buyerAddr, sellerAddr, sdk.Coins{coinToSend})
			if err != nil {
				return nil, err
			}

			credit := &ecocredit.MsgSend_SendCredits{
				BatchDenom:     sellOrder.BatchDenom,
				TradableAmount: creditsToReceive.String(),
				// TODO: handle auto-retire settings #621
				//RetiredAmount: creditsToRetire.String(),
				//RetirementLocation: retirementLocation,
			}

			// send credits to the buyer account
			err = s.sendEcocredits(ctx, credit, store, sellerAddr, buyerAddr)
			if err != nil {
				return nil, err
			}

			// get remaining credits in sell order
			creditsRemaining, err := creditsAvailable.Sub(creditsToReceive)
			if err != nil {
				return nil, err
			}

			if creditsRemaining.IsZero() {

				// delete sell order if no remaining credits
				s.sellOrderTable.Delete(ctx, sellOrder.OrderId)

			} else {
				sellOrder.Quantity = creditsRemaining.String()

				// update sell order quantity with remaining credits
				err = s.sellOrderTable.Update(ctx, sellOrder.OrderId, sellOrder)
				if err != nil {
					return nil, err
				}
			}

			// TODO: do we want to store a direct buy order? #623
			buyOrderID := s.buyOrderTable.Sequence().NextVal(ctx)
			buyOrderIds[i] = buyOrderID

			err = ctx.EventManager().EmitTypedEvent(&ecocredit.EventBuyOrderCreated{
				BuyOrderId:         buyOrderID,
				SellOrderId:        sellOrderId,
				Quantity:           order.Quantity,
				BidPrice:           order.BidPrice,
				DisableAutoRetire:  order.DisableAutoRetire,
				DisablePartialFill: order.DisablePartialFill,
			})
			if err != nil {
				return nil, err
			}

			err = ctx.EventManager().EmitTypedEvent(&ecocredit.EventBuyOrderFilled{
				BuyOrderId:  buyOrderID,
				SellOrderId: sellOrderId,
				BatchDenom:  sellOrder.BatchDenom,
				Quantity:    creditsToReceive.String(),
				TotalPrice:  &coinToSend,
			})
			if err != nil {
				return nil, err
			}

		// TODO: implement processing for filter option #623
		//case *ecocredit.MsgBuy_Order_Selection_Filter:

		default:
			return nil, sdkerrors.ErrInvalidRequest
		}

		ctx.GasMeter().ConsumeGas(gasCostPerIteration, "create buy order")
	}

	return &ecocredit.MsgBuyResponse{BuyOrderIds: buyOrderIds}, nil
}

// AllowAskDenom adds a new ask denom
func (s serverImpl) AllowAskDenom(goCtx context.Context, req *ecocredit.MsgAllowAskDenom) (*ecocredit.MsgAllowAskDenomResponse, error) {
	ctx := types.UnwrapSDKContext(goCtx)

	if req.RootAddress != authtypes.NewModuleAddress(govtypes.ModuleName).String() {
		return nil, sdkerrors.ErrUnauthorized.Wrap("root address must be governance module address")
	}

	err := s.askDenomTable.Create(ctx, &ecocredit.AskDenom{
		Denom:        req.Denom,
		DisplayDenom: req.DisplayDenom,
		Exponent:     req.Exponent,
	})
	if err != nil {
		return nil, err
	}

	err = ctx.EventManager().EmitTypedEvent(&ecocredit.EventAllowAskDenom{
		Denom:        req.Denom,
		DisplayDenom: req.DisplayDenom,
		Exponent:     req.Exponent,
	})
	if err != nil {
		return nil, err
	}

	return &ecocredit.MsgAllowAskDenomResponse{}, nil
}

func (s serverImpl) sendEcocredits(ctx types.Context, credit *ecocredit.MsgSend_SendCredits, store sdk.KVStore, senderAddr sdk.AccAddress, recipientAddr sdk.AccAddress) error {
	denom := batchDenomT(credit.BatchDenom)
	if !s.batchInfoTable.Has(ctx, orm.RowID(denom)) {
		return sdkerrors.ErrInvalidRequest.Wrapf("%s is not a valid credit batch denom", denom)
	}

	maxDecimalPlaces, err := s.getBatchPrecision(ctx, denom)
	if err != nil {
		return err
	}

	tradable, err := math.NewNonNegativeFixedDecFromString(credit.TradableAmount, maxDecimalPlaces)
	if err != nil {
		return err
	}

	retired, err := math.NewNonNegativeFixedDecFromString(credit.RetiredAmount, maxDecimalPlaces)
	if err != nil {
		return err
	}

	sum, err := tradable.Add(retired)
	if err != nil {
		return err
	}

	// subtract balance
	err = subAndSetDecimal(store, TradableBalanceKey(senderAddr, denom), sum)
	if err != nil {
		return err
	}

	// Add tradable balance
	err = addAndSetDecimal(store, TradableBalanceKey(recipientAddr, denom), tradable)
	if err != nil {
		return err
	}

	if !retired.IsZero() {
		// subtract retired from tradable supply
		err = subAndSetDecimal(store, TradableSupplyKey(denom), retired)
		if err != nil {
			return err
		}

		// Add retired balance
		err = retire(ctx, store, recipientAddr, denom, retired, credit.RetirementLocation)
		if err != nil {
			return err
		}

		// Add retired supply
		err = addAndSetDecimal(store, RetiredSupplyKey(denom), retired)
		if err != nil {
			return err
		}
	}

	err = ctx.EventManager().EmitTypedEvent(&ecocredit.EventReceive{
		Sender:         senderAddr.String(),
		Recipient:      recipientAddr.String(),
		BatchDenom:     string(denom),
		TradableAmount: tradable.String(),
		RetiredAmount:  retired.String(),
	})
	if err != nil {
		return err
	}

	return nil
}
