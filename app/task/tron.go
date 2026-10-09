package task

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"os"
	"sync"
	"time"

	"github.com/btcsuite/btcd/btcutil/base58"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/panjf2000/ants/v2"
	"github.com/shopspring/decimal"
	"github.com/smallnest/chanx"
	"github.com/spf13/cast"
	"github.com/v03413/bepusdt/app/conf"
	blockapi "github.com/v03413/bepusdt/app/core"
	"github.com/v03413/bepusdt/app/log"
	"github.com/v03413/bepusdt/app/model"
	"github.com/v03413/bepusdt/app/utils"
	"github.com/v03413/tronprotocol/api"
	"github.com/v03413/tronprotocol/core"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
)

var gasFreeUsdtTokenAddress = []byte{0xa6, 0x14, 0xf8, 0x03, 0xb6, 0xfd, 0x78, 0x09, 0x86, 0xa4, 0x2c, 0x78, 0xec, 0x9c, 0x7f, 0x77, 0xe6, 0xde, 0xd1, 0x3c}
var gasFreeOwnerAddress = []byte{0x41, 0x3b, 0x41, 0x50, 0x50, 0xb1, 0xe7, 0x9e, 0x38, 0x50, 0x7c, 0xb6, 0xe4, 0x8d, 0xac, 0xc2, 0x27, 0xaf, 0xfd, 0xd5, 0x0c}
var gasFreeContractAddress = []byte{0x41, 0x39, 0xdd, 0x12, 0xa5, 0x4e, 0x2b, 0xab, 0x7c, 0x82, 0xaa, 0x14, 0xa1, 0xe1, 0x58, 0xb3, 0x42, 0x63, 0xd2, 0xd5, 0x10}
var usdtTrc20ContractAddress = []byte{0x41, 0xa6, 0x14, 0xf8, 0x03, 0xb6, 0xfd, 0x78, 0x09, 0x86, 0xa4, 0x2c, 0x78, 0xec, 0x9c, 0x7f, 0x77, 0xe6, 0xde, 0xd1, 0x3c}
var usdcTrc20ContractAddress = []byte{0x41, 0x34, 0x87, 0xb6, 0x3d, 0x30, 0xb5, 0xb2, 0xc8, 0x7f, 0xb7, 0xff, 0xa8, 0xbc, 0xfa, 0xde, 0x38, 0xea, 0xac, 0x1a, 0xbe}

type tron struct {
	lastBlockNum         int
	blockConfirmedOffset int
	blockScanQueue       *chanx.UnboundedChan[int]
	conn                 map[string]*grpc.ClientConn
	connMu               sync.RWMutex
	retryMu              sync.Mutex
	retryAttempts        map[int]int
	retryScheduled       map[int]*time.Timer
	scanRPC              api.WalletClient // Optional local-test RPC; production uses client().
}

var tr tron

func init() {
	tr = newTron()
	Register(Task{Duration: time.Second, Callback: tr.blockDispatch})
	Register(Task{Duration: time.Second * 3, Callback: tr.syncBlocksForward})
	Register(Task{Duration: time.Second * 5, Callback: tr.tradeConfirmHandle})
	Register(Task{Duration: time.Second * 15, Callback: tr.lookbackBlocks})
}

func newTron() tron {
	return tron{
		lastBlockNum:         0,
		blockConfirmedOffset: 30,
		blockScanQueue:       chanx.NewUnboundedChan[int](context.Background(), 30),
		conn:                 make(map[string]*grpc.ClientConn),
		retryAttempts:        make(map[int]int),
		retryScheduled:       make(map[int]*time.Timer),
	}
}

// syncBlocksForward 正向同步区块
func (t *tron) syncBlocksForward(context.Context) {
	if t.syncBreak() {

		return
	}

	conn, err := t.client()
	if err != nil {
		log.Task.Error("grpc.NewClient", err)

		return
	}

	var ctx, cancel = context.WithTimeout(context.Background(), time.Second*15)
	block, err1 := api.NewWalletClient(conn).GetNowBlock2(ctx, nil)
	defer cancel()

	if err1 != nil {
		log.Task.Warn("GetNowBlock2 超时：", err1)

		return
	}

	if err := validateTronBlock(block); err != nil {
		log.Task.Error("GetNowBlock2", err)
		return
	}
	var now = int(block.GetBlockHeader().GetRawData().GetNumber())

	// 区块高度变化过大，强制丢块重扫
	if now-t.lastBlockNum > cast.ToInt(model.GetC(model.BlockHeightMaxDiff)) {
		t.lastBlockNum = now - 1
	}

	// 区块高度没有变化
	if now == t.lastBlockNum {

		return
	}

	// 待扫描区块入列
	for n := t.lastBlockNum + 1; n <= now; n++ {

		t.blockScanQueue.In <- n
	}

	t.lastBlockNum = now
}

func (t *tron) lookbackBlocks(ctx context.Context) {
	if t.syncBreak() {
		return
	}

	startAt, endAt, ok := getLookbackUnix(conf.Tron)
	if !ok {
		return
	}

	start, end := blockapi.New().GetBoundaryHeights(startAt, endAt, conf.Tron)
	for i := int(start); i <= int(end); i++ {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if t.syncBreak() {
			return
		}
		t.blockScanQueue.In <- i
		time.Sleep(time.Millisecond * 250) // 速率控制
	}
}

func (t *tron) blockDispatch(ctx context.Context) {
	p, err := ants.NewPoolWithFunc(2, t.blockParse)
	if err != nil {
		log.Task.Warn("Error creating pool:", err)

		return
	}

	defer p.Release()

	for {
		select {
		case <-ctx.Done():
			return
		case n, ok := <-t.blockScanQueue.Out:
			if !ok {
				return
			}
			if err := p.Invoke(n); err != nil {
				t.scheduleBlockRetry(n, 0)

				log.Task.Warn("Tron Error invoking process block:", err)
			}
		}
	}
}

func (t *tron) blockParse(n any) {
	num, ok := n.(int)
	if !ok {
		log.Task.Error("TRON block job has invalid height type")
		return
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			log.Task.Error("TRON block job panic at height ", num, ": ", recovered)
			conf.RecordFailure(conf.Tron)
			t.scheduleBlockRetry(num, 0)
		}
	}()

	c := t.scanRPC
	if c == nil {
		conn, err := t.client()
		if err != nil {
			log.Task.Error("grpc.NewClient", err)
			t.scheduleBlockRetry(num, 0)
			return
		}
		c = api.NewWalletClient(conn)
	}

	var ctx, cancel = context.WithTimeout(context.Background(), time.Second*5)
	bok, err2 := c.GetBlockByNum2(ctx, &api.NumberMessage{Num: int64(num)})
	cancel()
	if err2 != nil {
		conf.RecordFailure(conf.Tron)
		t.scheduleBlockRetry(num, 0)
		log.Task.Warn("GetBlockByNum2 ", err2)

		return
	}

	if err := validateTronBlock(bok); err != nil {
		log.Task.Error("GetBlockByNum2", err)
		conf.RecordFailure(conf.Tron)
		t.scheduleBlockRetry(num, 0)
		return
	}
	failed := false

	var resources = make([]resource, 0)
	var transfers = make([]transfer, 0)
	var timestamp = time.UnixMilli(bok.GetBlockHeader().GetRawData().GetTimestamp())
	for _, trans := range bok.GetTransactions() {
		if err := validateTronTransaction(trans); err != nil {
			log.Task.Error("TRON malformed block transaction", err)
			failed = true
			continue
		}
		if !trans.GetResult().GetResult() {

			continue
		}

		var itm = trans.GetTransaction()
		var id = hex.EncodeToString(trans.Txid)
		var hasTriggerContract bool
		for contractIndex, contract := range itm.GetRawData().GetContract() {
			// Native transfers do not have receipt logs. Keep them in a negative
			// namespace so they cannot collide with a TRC20 receipt log index.
			eventIndex := -1 - int64(contractIndex)
			// 资源代理 DelegateResourceContract
			if contract.GetType() == core.Transaction_Contract_DelegateResourceContract {
				var foo = &core.DelegateResourceContract{}
				err := contract.GetParameter().UnmarshalTo(foo)
				if err != nil {
					log.Task.Error("TRON contract decode", err)
					failed = true
					continue
				}

				resources = append(resources, resource{
					ID:           id,
					Type:         core.Transaction_Contract_DelegateResourceContract,
					Balance:      foo.Balance,
					ResourceCode: foo.Resource,
					FromAddress:  t.base58CheckEncode(foo.OwnerAddress),
					RecvAddress:  t.base58CheckEncode(foo.ReceiverAddress),
					Timestamp:    timestamp,
				})
			}

			// 资源回收 UnDelegateResourceContract
			if contract.GetType() == core.Transaction_Contract_UnDelegateResourceContract {
				var foo = &core.UnDelegateResourceContract{}
				err := contract.GetParameter().UnmarshalTo(foo)
				if err != nil {
					log.Task.Error("TRON contract decode", err)
					failed = true
					continue
				}

				resources = append(resources, resource{
					ID:           id,
					Type:         core.Transaction_Contract_UnDelegateResourceContract,
					Balance:      foo.Balance,
					ResourceCode: foo.Resource,
					FromAddress:  t.base58CheckEncode(foo.OwnerAddress),
					RecvAddress:  t.base58CheckEncode(foo.ReceiverAddress),
					Timestamp:    timestamp,
				})
			}

			// TRX转账交易
			if contract.GetType() == core.Transaction_Contract_TransferContract {
				var foo = &core.TransferContract{}
				err := contract.GetParameter().UnmarshalTo(foo)
				if err != nil {
					log.Task.Error("TRON contract decode", err)
					failed = true
					continue
				}

				transfers = append(transfers, transfer{
					Network:     conf.Tron,
					TxHash:      id,
					Amount:      decimal.NewFromBigInt(new(big.Int).SetInt64(foo.Amount), -6),
					FromAddress: t.base58CheckEncode(foo.OwnerAddress),
					RecvAddress: t.base58CheckEncode(foo.ToAddress),
					Timestamp:   timestamp,
					TradeType:   model.TronTrx,
					BlockNum:    cast.ToInt(num),
					EventIndex:  eventIndex,
				})
			}

			// 触发智能合约
			if contract.GetType() == core.Transaction_Contract_TriggerSmartContract {
				hasTriggerContract = true
				// A call may emit multiple Transfer logs. Event identity must use
				// the receipt log position, never calldata or contract position.
				continue
				var foo = &core.TriggerSmartContract{}
				if err := contract.GetParameter().UnmarshalTo(foo); err != nil {

					continue
				}

				data := foo.GetData()
				if len(data) < 4 {

					continue
				}

				// Gas Free 钱包 合约授权转账
				if bytes.Equal(foo.OwnerAddress, gasFreeOwnerAddress) && bytes.Equal(foo.ContractAddress, gasFreeContractAddress) {
					from, receiver, amount := t.gasFreePermitTransfer(data)
					if amount != nil {
						transfers = append(transfers, transfer{
							Network:     conf.Tron,
							TxHash:      id,
							Amount:      decimal.NewFromBigInt(amount, conf.UsdtTronDecimals),
							FromAddress: from,
							RecvAddress: receiver,
							Timestamp:   timestamp,
							TradeType:   model.UsdtTrc20,
							BlockNum:    cast.ToInt(num),
							EventIndex:  eventIndex,
						})
					}
				}

				// trc20 合约解析
				var tradeType model.TradeType = "None"
				if bytes.Equal(foo.GetContractAddress(), usdtTrc20ContractAddress) {
					tradeType = model.UsdtTrc20
				} else if bytes.Equal(foo.GetContractAddress(), usdcTrc20ContractAddress) {
					tradeType = model.UsdcTrc20
				} else {
					continue
				}

				if bytes.Equal(data[:4], []byte{0xa9, 0x05, 0x9c, 0xbb}) { //  a9059cbb transfer
					receiver, amount := t.parseTrc20ContractTransfer(data)
					if amount != nil {
						transfers = append(transfers, transfer{
							Network:     conf.Tron,
							TxHash:      id,
							Amount:      decimal.NewFromBigInt(amount, model.GetTradeDecimal(tradeType)),
							FromAddress: t.base58CheckEncode(foo.OwnerAddress),
							RecvAddress: receiver,
							Timestamp:   timestamp,
							TradeType:   tradeType,
							BlockNum:    cast.ToInt(num),
							EventIndex:  eventIndex,
						})
					}
				}
				if bytes.Equal(data[:4], []byte{0x23, 0xb8, 0x72, 0xdd}) { //  transferFrom (23b872dd)
					from, to, amount := t.parseTrc20ContractTransferFrom(data)
					if amount != nil {
						transfers = append(transfers, transfer{
							Network:     conf.Tron,
							TxHash:      id,
							Amount:      decimal.NewFromBigInt(amount, model.GetTradeDecimal(tradeType)),
							FromAddress: from,
							RecvAddress: to,
							Timestamp:   timestamp,
							TradeType:   tradeType,
							BlockNum:    cast.ToInt(num),
							EventIndex:  eventIndex,
						})
					}
				}
			}
		}

		if hasTriggerContract {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			info, err := c.GetTransactionInfoById(ctx, &api.BytesMessage{Value: trans.Txid})
			cancel()
			if err != nil {
				log.Task.Error("GetTransactionInfoById while scanning TRC20 logs", err)
				failed = true
				continue
			}
			success, err := tronReceiptSucceeded(info)
			if err != nil {
				log.Task.Error("TRON receipt", err)
				failed = true
				continue
			}
			if success {
				transfers = append(transfers, t.parseTrc20ReceiptLogs(info, id, timestamp, num)...)
			}
		}
	}

	if len(transfers) > 0 {
		transferQueue.In <- transfers
	}
	if len(resources) > 0 {
		resourceQueue.In <- resources
	}
	if failed {
		conf.RecordFailure(conf.Tron)
		t.scheduleBlockRetry(num, 0)
		return
	}
	conf.RecordSuccess(conf.Tron, cast.ToString(num))
	t.resetBlockRetry(num)

	log.Task.Info(fmt.Sprintf("区块扫描完成(Tron): %d 成功率：%s", num, conf.GetSuccessRate(conf.Tron)))
}

const trc20TransferTopic = "ddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"

// parseTrc20ReceiptLogs uses the zero-based position in TransactionInfo.Log as
// the stable event index persisted in bep_chain_event.event_index.
func (t *tron) parseTrc20ReceiptLogs(info *core.TransactionInfo, txHash string, timestamp time.Time, blockNum int) []transfer {
	transfers := make([]transfer, 0)
	for logIndex, eventLog := range info.GetLog() {
		topics := eventLog.GetTopics()
		if len(topics) < 3 || hex.EncodeToString(topics[0]) != trc20TransferTopic {
			continue
		}

		contractAddress := last20(eventLog.GetAddress())
		var tradeType model.TradeType
		switch {
		case bytes.Equal(contractAddress, last20(usdtTrc20ContractAddress)):
			tradeType = model.UsdtTrc20
		case bytes.Equal(contractAddress, last20(usdcTrc20ContractAddress)):
			tradeType = model.UsdcTrc20
		default:
			continue
		}

		from := append([]byte{0x41}, last20(topics[1])...)
		to := append([]byte{0x41}, last20(topics[2])...)
		amount := new(big.Int).SetBytes(eventLog.GetData())
		transfers = append(transfers, transfer{
			Network:     conf.Tron,
			TxHash:      txHash,
			Amount:      decimal.NewFromBigInt(amount, model.GetTradeDecimal(tradeType)),
			FromAddress: t.base58CheckEncode(from),
			RecvAddress: t.base58CheckEncode(to),
			Timestamp:   timestamp,
			TradeType:   tradeType,
			BlockNum:    blockNum,
			EventIndex:  int64(logIndex),
		})
	}

	return transfers
}

func last20(value []byte) []byte {
	if len(value) <= 20 {
		return value
	}

	return value[len(value)-20:]
}

func (t *tron) parseTrc20ContractTransfer(data []byte) (string, *big.Int) {
	if len(data) != 68 {

		return "", nil
	}

	receiver := t.base58CheckEncode(append([]byte{0x41}, data[16:36]...))
	amount := big.NewInt(0).SetBytes(data[36:68])

	return receiver, amount
}

func (t *tron) parseTrc20ContractTransferFrom(data []byte) (string, string, *big.Int) {
	if len(data) != 100 {

		return "", "", nil
	}

	from := t.base58CheckEncode(append([]byte{0x41}, data[16:36]...))
	to := t.base58CheckEncode(append([]byte{0x41}, data[48:68]...))
	amount := big.NewInt(0).SetBytes(data[68:100])

	return from, to, amount
}

func (t *tron) gasFreePermitTransfer(data []byte) (string, string, *big.Int) {
	// https://tronscan.org/#/contract/TFFAMQLZybALaLb4uxHA9RBE7pxhUAjF3U/code?func=Tab-proxywrite-F3proxyNonePayable
	if len(data) != 420 {

		return "", "", nil
	}

	if !bytes.Equal(data[:4], []byte{0x6f, 0x21, 0xb8, 0x98}) {
		// not permitTransfer (6f21b898) function

		return "", "", nil
	}

	if !bytes.Equal(data[16:36], gasFreeUsdtTokenAddress) {
		// not gas free usdt token address

		return "", "", nil
	}

	user := t.base58CheckEncode(append([]byte{0x41}, data[48:68]...))
	receiver := t.base58CheckEncode(append([]byte{0x41}, data[80:100]...))
	amount := big.NewInt(0).SetBytes(data[100:132])

	return user, receiver, amount
}

func (t *tron) tradeConfirmHandle(ctx context.Context) {
	var orders = getConfirmingOrders([]model.TradeType{model.TronTrx, model.UsdtTrc20, model.UsdcTrc20})

	var handle = func(o model.Order) error {
		var c api.WalletClient
		if os.Getenv("BEPUSDT_TRON_HTTPS_CONFIG_FILE") != "" {
			httpClient, err := newTronHTTPSClient()
			if err != nil {
				return err
			}
			defer httpClient.close()
			c = httpClient
		} else {
			conn, err := t.client()
			if err != nil {
				return fmt.Errorf("TRON client: %w", err)
			}
			c = api.NewWalletClient(conn)
		}

		idBytes, err := hex.DecodeString(o.RefHash)
		if err != nil || len(idBytes) != 32 {
			return fmt.Errorf("invalid TRON transaction hash")
		}

		success, err := tronConfirmWithPolicy(ctx, c, o, idBytes)
		if err != nil {
			return err
		}
		if success {
			return markFinalConfirmed(o)
		}
		return nil
	}

	processTronOrders(orders, handle, func(o model.Order, err error) {
		log.Task.Error("TRON confirmation order ", o.ID, ": ", err)
	})
}

func (t *tron) base58CheckEncode(input []byte) string {
	checksum := chainhash.DoubleHashB(input)
	checksum = checksum[:4]

	input = append(input, checksum...)

	return base58.Encode(input)
}

func (t *tron) syncBreak() bool {
	// HTTPS mode uses the bounded recipient indexer; never send provider keys over plaintext gRPC.
	if os.Getenv("BEPUSDT_TRON_HTTPS_CONFIG_FILE") != "" {
		return true
	}
	if t.blockScanQueue.Len() >= blockQueueLimit {
		log.Task.Warn("tron 同步阻塞，当前区块消费堆积数量：", t.blockScanQueue.Len())

		return true
	}

	if mqttSubscribed(conf.Tron) {
		return false
	}

	trade := []model.TradeType{model.TronTrx, model.UsdtTrc20, model.UsdcTrc20}
	if hasLookbackOrders(trade) {

		return false
	}

	var count int64 = 0
	model.Db.Model(&model.Wallet{}).Where("other_notify = ? and trade_type in (?)", model.WaOtherEnable, trade).Count(&count)
	if count > 0 {

		return false
	}

	return true
}

func (t *tron) scheduleBlockRetry(num int, delay time.Duration) {
	t.retryMu.Lock()
	if _, ok := t.retryScheduled[num]; ok {
		t.retryMu.Unlock()

		return
	}

	attempt := t.retryAttempts[num] + 1
	t.retryAttempts[num] = attempt

	if delay <= 0 {
		delay = tronRetryDelay(attempt)
	}

	var timer *time.Timer
	timer = time.AfterFunc(delay, func() {
		t.retryMu.Lock()
		if t.retryScheduled[num] != timer {
			t.retryMu.Unlock()

			return
		}
		delete(t.retryScheduled, num)
		t.retryMu.Unlock()

		if t.blockScanQueue.Len() >= blockQueueLimit {
			log.Task.Warn("Tron 重试延后，当前区块消费堆积数量：", t.blockScanQueue.Len())
			t.scheduleBlockRetry(num, delay)

			return
		}

		t.blockScanQueue.In <- num
	})
	t.retryScheduled[num] = timer
	t.retryMu.Unlock()
}

func (t *tron) resetBlockRetry(num int) {
	t.retryMu.Lock()
	defer t.retryMu.Unlock()

	delete(t.retryAttempts, num)
	if timer, ok := t.retryScheduled[num]; ok {
		timer.Stop()
		delete(t.retryScheduled, num)
	}
}

func tronRetryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 5 {
		attempt = 5
	}

	return time.Duration(1<<uint(attempt-1)) * 5 * time.Second
}

func (t *tron) client() (*grpc.ClientConn, error) {
	var endpoint = model.Endpoint(conf.Tron)

	t.connMu.RLock()
	if c, ok := t.conn[endpoint]; ok {
		state := c.GetState()
		if state == connectivity.Ready || state == connectivity.Idle {
			t.connMu.RUnlock()

			return c, nil
		}

		t.connMu.RUnlock()
	} else {
		t.connMu.RUnlock()
	}

	t.connMu.Lock()
	defer t.connMu.Unlock()

	if c, ok := t.conn[endpoint]; ok {
		state := c.GetState()
		if state == connectivity.Ready || state == connectivity.Idle {

			return c, nil
		}

		c.Close()
	}

	conn, err := utils.NewTronGrpcClient(endpoint, model.GetTronGridApiKeys())
	if err != nil {

		return nil, fmt.Errorf("连接失败: %w", err)
	}

	t.conn[endpoint] = conn
	log.Task.Info("Tron gRPC 连接已建立:", endpoint)

	return conn, nil
}
