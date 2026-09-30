package task

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/v03413/bepusdt/app/model"
	"github.com/v03413/tronprotocol/api"
	"github.com/v03413/tronprotocol/core"
)

func validateTronBlock(block *api.BlockExtention) error {
	if block == nil || block.GetBlockHeader() == nil || block.GetBlockHeader().GetRawData() == nil {
		return fmt.Errorf("TRON block missing header/raw data")
	}
	return nil
}

func validateTronTransaction(tx *api.TransactionExtention) error {
	if tx == nil || tx.GetResult() == nil || tx.GetTransaction() == nil || tx.GetTransaction().GetRawData() == nil || len(tx.GetTxid()) != 32 {
		return fmt.Errorf("TRON transaction missing result, transaction, raw data or transaction ID")
	}
	for _, contract := range tx.GetTransaction().GetRawData().GetContract() {
		if contract == nil || contract.GetParameter() == nil {
			return fmt.Errorf("TRON transaction missing contract/parameter")
		}
	}
	return nil
}

func tronTransactionSucceeded(tx *core.Transaction) (bool, error) {
	if tx == nil || len(tx.GetRet()) == 0 {
		return false, fmt.Errorf("TRON transaction missing result")
	}
	for _, result := range tx.GetRet() {
		if result == nil {
			return false, fmt.Errorf("TRON transaction contains nil result")
		}
		if result.GetContractRet() != core.Transaction_Result_SUCCESS {
			return false, nil
		}
	}
	return true, nil
}

func tronReceiptSucceeded(info *core.TransactionInfo) (bool, error) {
	if info == nil || info.GetReceipt() == nil {
		return false, fmt.Errorf("TRON transaction info missing receipt")
	}
	return info.GetReceipt().GetResult() == core.Transaction_Result_SUCCESS, nil
}

// Recovery is limited to one block/order job; the caller must log and retry.
func tronJob(job func() error) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("TRON job panic: %v", recovered)
		}
	}()
	return job()
}

func tronConfirmRPC(ctx context.Context, client api.WalletClient, order model.Order, id []byte) (bool, error) {
	rpcCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if order.TradeType == model.TronTrx {
		tx, err := client.GetTransactionById(rpcCtx, &api.BytesMessage{Value: id})
		if err != nil {
			return false, fmt.Errorf("GetTransactionById: %w", err)
		}
		return tronTransactionSucceeded(tx)
	}
	info, err := client.GetTransactionInfoById(rpcCtx, &api.BytesMessage{Value: id})
	if err != nil {
		return false, fmt.Errorf("GetTransactionInfoById: %w", err)
	}
	return tronReceiptSucceeded(info)
}

func processTronOrders(orders []model.Order, handle func(model.Order) error, onError func(model.Order, error)) {
	var wg sync.WaitGroup
	for _, order := range orders {
		wg.Add(1)
		go func(order model.Order) {
			defer wg.Done()
			if err := tronJob(func() error { return handle(order) }); err != nil {
				onError(order, err)
			}
		}(order)
	}
	wg.Wait()
}
