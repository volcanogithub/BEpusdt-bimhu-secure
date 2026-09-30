package task

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	applog "github.com/v03413/bepusdt/app/log"

	"github.com/v03413/bepusdt/app/model"
	"github.com/v03413/tronprotocol/api"
	"github.com/v03413/tronprotocol/core"
	"google.golang.org/grpc"
)

type b22Wallet struct {
	api.WalletClient
	tx         *core.Transaction
	info       *core.TransactionInfo
	err        error
	timeout    bool
	block      *api.BlockExtention
	panicBlock bool
}

func (w b22Wallet) GetBlockByNum2(context.Context, *api.NumberMessage, ...grpc.CallOption) (*api.BlockExtention, error) {
	if w.panicBlock {
		panic("injected block RPC panic")
	}
	return w.block, w.err
}

func (w b22Wallet) GetTransactionById(ctx context.Context, _ *api.BytesMessage, _ ...grpc.CallOption) (*core.Transaction, error) {
	if w.timeout {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return w.tx, w.err
}
func (w b22Wallet) GetTransactionInfoById(ctx context.Context, _ *api.BytesMessage, _ ...grpc.CallOption) (*core.TransactionInfo, error) {
	if w.timeout {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return w.info, w.err
}

func TestB22EmptyTransactionResult(t *testing.T) {
	for _, tx := range []*core.Transaction{nil, {}, {Ret: []*core.Transaction_Result{nil}}} {
		success, err := tronTransactionSucceeded(tx)
		if err == nil || success {
			t.Fatal("missing result accepted")
		}
	}
}

func TestB22MissingReceipt(t *testing.T) {
	for _, info := range []*core.TransactionInfo{nil, {}} {
		success, err := tronReceiptSucceeded(info)
		if err == nil || success {
			t.Fatal("missing receipt accepted")
		}
	}
}

func TestB22RPCTimeoutAndError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	for _, kind := range []model.TradeType{model.TronTrx, model.UsdtTrc20} {
		success, err := tronConfirmRPC(ctx, b22Wallet{timeout: true}, model.Order{TradeType: kind}, nil)
		if success || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("timeout/cancellation not propagated: %v", err)
		}
		success, err = tronConfirmRPC(context.Background(), b22Wallet{err: context.DeadlineExceeded}, model.Order{TradeType: kind}, nil)
		if success || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("RPC timeout not propagated: %v", err)
		}
	}
}

func TestB22OneHundredOrdersErrorIsolation(t *testing.T) {
	for _, panicCase := range []bool{false, true} {
		orders := make([]model.Order, 100)
		for i := range orders {
			orders[i].ID = int64(i + 1)
		}
		var processed, failures atomic.Int32
		processTronOrders(orders, func(o model.Order) error {
			if o.ID == 50 {
				if panicCase {
					panic("injected order panic")
				}
				_, err := tronConfirmRPC(context.Background(), b22Wallet{err: errors.New("RPC unavailable")}, o, nil)
				return err
			}
			processed.Add(1)
			return nil
		}, func(_ model.Order, _ error) { failures.Add(1) })
		if processed.Load() != 99 || failures.Load() != 1 {
			t.Fatalf("processed=%d failures=%d", processed.Load(), failures.Load())
		}
	}
}

func TestB22MalformedInputsDoNotPanic(t *testing.T) {
	for _, block := range []*api.BlockExtention{nil, {}} {
		if err := validateTronBlock(block); err == nil {
			t.Fatal("empty block accepted")
		}
	}
	for _, tx := range []*api.TransactionExtention{nil, {}, {Result: &api.Return{Result: true}}} {
		if err := validateTronTransaction(tx); err == nil {
			t.Fatal("malformed transaction accepted")
		}
	}
	for size := 0; size < 450; size++ {
		data := make([]byte, size)
		tr.parseTrc20ContractTransfer(data)
		tr.parseTrc20ContractTransferFrom(data)
		tr.gasFreePermitTransfer(data)
	}
	tr.parseTrc20ReceiptLogs(nil, "", time.Time{}, 0)
}

func TestB22BlockFailureSchedulesRetry(t *testing.T) {
	old := applog.Task
	applog.Task = logrus.New()
	applog.Task.SetOutput(io.Discard)
	defer func() { applog.Task = old }()
	for _, rpc := range []b22Wallet{{}, {block: &api.BlockExtention{}}, {err: context.DeadlineExceeded}, {panicBlock: true}} {
		scanner := newTron()
		scanner.scanRPC = rpc
		scanner.blockParse(123)
		scanner.retryMu.Lock()
		attempts := scanner.retryAttempts[123]
		timer := scanner.retryScheduled[123]
		scanner.retryMu.Unlock()
		scanner.resetBlockRetry(123)
		if attempts != 1 || timer == nil {
			t.Fatal("actual block failure did not schedule retry")
		}
	}
}
