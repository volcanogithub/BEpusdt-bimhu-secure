package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/v03413/bepusdt/app/model"
	"github.com/v03413/tronprotocol/api"
	"github.com/v03413/tronprotocol/core"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protojson"
	"gorm.io/gorm"
)

type b23Wallet struct {
	api.WalletClient
	head int64
	info *core.TransactionInfo
	err error
	receiptCalls atomic.Int32
}

func (w *b23Wallet) GetNowBlock2(context.Context, *api.EmptyMessage, ...grpc.CallOption) (*api.BlockExtention, error) {
	if w.err != nil { return nil, w.err }
	block := &api.BlockExtention{}
	err := protojson.Unmarshal([]byte(fmt.Sprintf("{\"blockHeader\":{\"rawData\":{\"number\":%d}}}", w.head)), block)
	return block, err
}

func (w *b23Wallet) GetTransactionInfoById(context.Context, *api.BytesMessage, ...grpc.CallOption) (*core.TransactionInfo, error) {
	w.receiptCalls.Add(1)
	return w.info, w.err
}

func b23PolicyDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "policy.db")), &gorm.Config{})
	if err != nil { t.Fatal(err) }
	sqlDB, _ := db.DB()
	t.Cleanup(func() { sqlDB.Close() })
	model.Db = db
	if err := db.AutoMigrate(&model.Conf{}); err != nil { t.Fatal(err) }
	if err := db.Create(&model.Conf{K: model.ConfirmationDepthTron, V: "20"}).Error; err != nil { t.Fatal(err) }
	model.RefreshC()
	return db
}

func TestB23TronRPCConfirmationGate(t *testing.T) {
	b23PolicyDB(t)
	id := make([]byte, 32)
	id[0] = 1
	info := &core.TransactionInfo{Id: id, BlockNumber: 100}
	if err := protojson.Unmarshal([]byte("{\"receipt\":{\"result\":\"SUCCESS\"}}"), info); err != nil { t.Fatal(err) }
	// Unmarshal resets the message: restore bound transaction identity.
	info.Id, info.BlockNumber = id, 100
	wallet := &b23Wallet{head: 119, info: info}
	order := model.Order{TradeType: model.UsdtTrc20, RefBlockNum: 100}
	success, err := tronConfirmWithPolicy(context.Background(), wallet, order, id)
	if err != nil || success || wallet.receiptCalls.Load() != 0 { t.Fatal("insufficient confirmations entered success path") }
	wallet.head = 120
	success, err = tronConfirmWithPolicy(context.Background(), wallet, order, id)
	if err != nil || !success { t.Fatalf("threshold did not allow downstream success: %v", err) }
	wallet.info.BlockNumber = 101
	if success, err := tronConfirmWithPolicy(context.Background(), wallet, order, id); success || err == nil { t.Fatal("rebound receipt accepted") }
	wallet.err = errors.New("head unavailable")
	if success, err := tronConfirmWithPolicy(context.Background(), wallet, order, id); success || err == nil { t.Fatal("failed head accepted") }
}

func TestB23EVMFreshHeadValidation(t *testing.T) {
	db := b23PolicyDB(t)
	response := "0x78"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil { t.Error(err) }
		if request["method"] != "eth_blockNumber" { t.Error("wrong RPC method") }
		fmt.Fprintf(w, "{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":%q}", response)
	}))
	defer server.Close()
	if err := db.Create(&model.Conf{K: model.RpcEndpointEthereum, V: server.URL}).Error; err != nil { t.Fatal(err) }
	model.RefreshC()
	e := evm{Network: "ethereum", Client: server.Client()}
	head, err := e.confirmationHead(context.Background())
	if err != nil || head != 120 { t.Fatalf("head=%d err=%v", head, err) }
	response = "bad"
	if _, err := e.confirmationHead(context.Background()); err == nil { t.Fatal("malformed head accepted") }
}
