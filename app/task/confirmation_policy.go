package task

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/v03413/bepusdt/app/model"
	"github.com/v03413/tronprotocol/api"
)

// Fresh RPC heads avoid treating an old scanner height as current after a reorg.
func tronConfirmWithPolicy(ctx context.Context, client api.WalletClient, order model.Order, id []byte) (bool, error) {
	rpcCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err := model.RequiredConfirmationDepth("tron"); err != nil { return false, err }
	head, err := client.GetNowBlock2(rpcCtx, nil)
	if err != nil { return false, fmt.Errorf("TRON confirmation head: %w", err) }
	if err := validateTronBlock(head); err != nil { return false, err }
	ready, err := model.ConfirmationDepthReached("tron", head.GetBlockHeader().GetRawData().GetNumber(), int64(order.RefBlockNum))
	if err != nil || !ready { return false, err }
	info, err := client.GetTransactionInfoById(rpcCtx, &api.BytesMessage{Value: id})
	if err != nil { return false, fmt.Errorf("TRON confirmation inclusion: %w", err) }
	if info == nil || !bytes.Equal(info.GetId(), id) || info.GetBlockNumber() != int64(order.RefBlockNum) {
		return false, fmt.Errorf("TRON receipt inclusion does not match bound transaction")
	}
	if order.TradeType == model.TronTrx { return tronConfirmRPC(rpcCtx, client, order, id) }
	return tronReceiptSucceeded(info)
}

func parseConfirmationHeight(raw string) (int64, error) {
	if !strings.HasPrefix(raw, "0x") || len(raw) <= 2 { return 0, fmt.Errorf("invalid RPC block height") }
	height, err := strconv.ParseInt(raw[2:], 16, 64)
	if err != nil || height <= 0 { return 0, fmt.Errorf("invalid RPC block height") }
	return height, nil
}

func (e *evm) confirmationHead(ctx context.Context) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.rpcEndpoint(),
		strings.NewReader("{\"jsonrpc\":\"2.0\",\"method\":\"eth_blockNumber\",\"params\":[],\"id\":1}"))
	if err != nil { return 0, err }
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.Client.Do(req)
	if err != nil { return 0, err }
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK { return 0, fmt.Errorf("EVM head HTTP status %d", resp.StatusCode) }
	var result struct { Result string; Error json.RawMessage }
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil { return 0, err }
	if len(result.Error) != 0 && string(result.Error) != "null" { return 0, fmt.Errorf("EVM head RPC error") }
	return parseConfirmationHeight(result.Result)
}
