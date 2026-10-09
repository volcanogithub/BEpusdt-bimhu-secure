package task

// HTTPS observation is a BE hint source only. Payment still independently proves
// the full receipt and canonical block through its strict two-provider verifier.
import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/v03413/bepusdt/app/log"
	"github.com/v03413/bepusdt/app/model"
	"github.com/v03413/tronprotocol/api"
	"github.com/v03413/tronprotocol/core"
	"google.golang.org/grpc"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const httpsObserverRecipient = "TDwFg68T3mKo7WzmBexJRDpsmi9EKJZN9e"
const httpsObserverContract = "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t"

type tronHTTPSConfig struct {
	BaseURL    string `json:"base_url"`
	APIKeyFile string `json:"api_key_file"`
}
type tronHTTPSClient struct {
	api.WalletClient
	base, key string
	http      *http.Client
}

var httpsObserverMu sync.Mutex

func privateObserverFile(name string) ([]byte, error) {
	st, e := os.Lstat(name)
	if e != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0077 != 0 || st.Size() > 16384 {
		return nil, errors.New("HTTPS_OBSERVER_PRIVATE_FILE_REQUIRED")
	}
	b, e := os.ReadFile(name)
	if e != nil {
		return nil, errors.New("HTTPS_OBSERVER_PRIVATE_FILE_UNAVAILABLE")
	}
	return b, nil
}
func newTronHTTPSClient() (*tronHTTPSClient, error) {
	raw, e := privateObserverFile(os.Getenv("BEPUSDT_TRON_HTTPS_CONFIG_FILE"))
	if e != nil {
		return nil, e
	}
	var c tronHTTPSConfig
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&c) != nil || d.Decode(new(any)) != io.EOF || c.BaseURL != "https://api.trongrid.io" {
		return nil, errors.New("HTTPS_OBSERVER_MAINNET_ORIGIN_REQUIRED")
	}
	key, e := privateObserverFile(c.APIKeyFile)
	if e != nil {
		return nil, e
	}
	k := strings.TrimSpace(string(key))
	if len(k) < 16 || len(k) > 512 || strings.ContainsAny(k, "\r\n") {
		return nil, errors.New("HTTPS_OBSERVER_KEY_INVALID")
	}
	return &tronHTTPSClient{base: c.BaseURL, key: k, http: &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("HTTPS_OBSERVER_REDIRECT_REJECTED") }}}, nil
}
func (c *tronHTTPSClient) close() {
	if t, ok := c.http.Transport.(*http.Transport); ok {
		t.CloseIdleConnections()
	}
}
func (c *tronHTTPSClient) read(ctx context.Context, method, path string, body any, out any) error {
	var data []byte
	if body != nil {
		var e error
		data, e = json.Marshal(body)
		if e != nil {
			return errors.New("HTTPS_OBSERVER_REQUEST_INVALID")
		}
	}
	if !strings.HasPrefix(path, "/wallet/") && !strings.HasPrefix(path, "/v1/accounts/") {
		return errors.New("HTTPS_OBSERVER_PATH_REJECTED")
	}
	req, e := http.NewRequestWithContext(ctx, method, c.base+path, bytes.NewReader(data))
	if e != nil {
		return errors.New("HTTPS_OBSERVER_REQUEST_INVALID")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("TRON-PRO-API-KEY", c.key)
	resp, e := c.http.Do(req)
	if e != nil {
		return errors.New("HTTPS_OBSERVER_REQUEST_UNAVAILABLE")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTPS_OBSERVER_HTTP_%d", resp.StatusCode)
	}
	raw, e := io.ReadAll(io.LimitReader(resp.Body, (4<<20)+1))
	if e != nil || len(raw) > 4<<20 {
		return errors.New("HTTPS_OBSERVER_RESPONSE_LIMIT")
	}
	var failure map[string]json.RawMessage
	if json.Unmarshal(raw, &failure) != nil || len(failure["Error"]) > 0 || len(failure["error"]) > 0 {
		return errors.New("HTTPS_OBSERVER_RPC_ERROR")
	}
	if json.Unmarshal(raw, out) != nil {
		return errors.New("HTTPS_OBSERVER_RESPONSE_INVALID")
	}
	return nil
}

type observerBlock struct {
	BlockID string `json:"blockID"`
	Header  struct {
		Raw struct {
			Number    int64 `json:"number"`
			Timestamp int64 `json:"timestamp"`
		} `json:"raw_data"`
	} `json:"block_header"`
}

func (c *tronHTTPSClient) block(ctx context.Context, path string, body any) (*api.BlockExtention, error) {
	var b observerBlock
	if e := c.read(ctx, "POST", path, body, &b); e != nil {
		return nil, e
	}
	id, e := hex.DecodeString(b.BlockID)
	if e != nil || len(id) != 32 || b.Header.Raw.Number <= 0 || b.Header.Raw.Timestamp < 1000000000000 {
		return nil, errors.New("HTTPS_OBSERVER_CANONICAL_BLOCK_INVALID")
	}
	return &api.BlockExtention{Blockid: id, BlockHeader: &core.BlockHeader{RawData: &core.BlockHeader_Raw{Number: b.Header.Raw.Number, Timestamp: b.Header.Raw.Timestamp}}}, nil
}
func (c *tronHTTPSClient) GetNowBlock2(ctx context.Context, _ *api.EmptyMessage, _ ...grpc.CallOption) (*api.BlockExtention, error) {
	return c.block(ctx, "/wallet/getnowblock", map[string]any{})
}
func (c *tronHTTPSClient) GetBlockByNum2(ctx context.Context, n *api.NumberMessage, _ ...grpc.CallOption) (*api.BlockExtention, error) {
	b, e := c.block(ctx, "/wallet/getblockbynum", map[string]any{"num": n.Num})
	if e == nil && b.GetBlockHeader().GetRawData().GetNumber() != n.Num {
		return nil, errors.New("HTTPS_OBSERVER_BLOCK_NUMBER_MISMATCH")
	}
	return b, e
}

type observerReceipt struct {
	ID          string `json:"id"`
	BlockNumber int64  `json:"blockNumber"`
	BlockTime   int64  `json:"blockTimeStamp"`
	Receipt     struct {
		Result string `json:"result"`
	} `json:"receipt"`
	Result string `json:"result"`
	Logs   []struct {
		Address string   `json:"address"`
		Topics  []string `json:"topics"`
		Data    string   `json:"data"`
	} `json:"log"`
}

func decodeObserverReceipt(v observerReceipt, want []byte) (*core.TransactionInfo, error) {
	id, e := hex.DecodeString(v.ID)
	if e != nil || len(id) != 32 || !bytes.Equal(id, want) || v.BlockNumber <= 0 || v.BlockTime < 1000000000000 || v.Receipt.Result != "SUCCESS" || v.Result == "FAILED" {
		return nil, errors.New("HTTPS_OBSERVER_RECEIPT_NOT_SUCCESS")
	}
	out := &core.TransactionInfo{Id: id, BlockNumber: v.BlockNumber, BlockTimeStamp: v.BlockTime, Receipt: &core.ResourceReceipt{Result: core.Transaction_Result_SUCCESS}}
	for _, l := range v.Logs {
		address, e := hex.DecodeString(l.Address)
		if e != nil || (len(address) != 20 && len(address) != 21) {
			return nil, errors.New("HTTPS_OBSERVER_LOG_INVALID")
		}
		data, e := hex.DecodeString(l.Data)
		if e != nil {
			return nil, errors.New("HTTPS_OBSERVER_LOG_INVALID")
		}
		entry := &core.TransactionInfo_Log{Address: address, Data: data}
		for _, topic := range l.Topics {
			b, e := hex.DecodeString(topic)
			if e != nil || len(b) != 32 {
				return nil, errors.New("HTTPS_OBSERVER_LOG_INVALID")
			}
			entry.Topics = append(entry.Topics, b)
		}
		out.Log = append(out.Log, entry)
	}
	return out, nil
}
func (c *tronHTTPSClient) GetTransactionInfoById(ctx context.Context, id *api.BytesMessage, _ ...grpc.CallOption) (*core.TransactionInfo, error) {
	if len(id.Value) != 32 {
		return nil, errors.New("HTTPS_OBSERVER_TX_INVALID")
	}
	var r observerReceipt
	if e := c.read(ctx, "POST", "/wallet/gettransactioninfobyid", map[string]string{"value": hex.EncodeToString(id.Value)}, &r); e != nil {
		return nil, e
	}
	return decodeObserverReceipt(r, id.Value)
}
func (c *tronHTTPSClient) GetTransactionById(context.Context, *api.BytesMessage, ...grpc.CallOption) (*core.Transaction, error) {
	return nil, errors.New("HTTPS_OBSERVER_USDT_ONLY")
}

func init() { Register(Task{Duration: 15 * time.Second, Callback: tronHTTPSObserve}) }
func tronHTTPSObserve(ctx context.Context) {
	if os.Getenv("BEPUSDT_TRON_HTTPS_CONFIG_FILE") == "" || !httpsObserverMu.TryLock() {
		return
	}
	defer httpsObserverMu.Unlock()
	var orders []model.Order
	if e := model.Db.Where("status = ? AND trade_type = ? AND address = ?", model.OrderStatusWaiting, model.UsdtTrc20, httpsObserverRecipient).Order("created_at asc").Limit(100).Find(&orders).Error; e != nil || len(orders) == 0 {
		return
	}
	c, e := newTronHTTPSClient()
	if e != nil {
		log.Task.Error("HTTPS observer configuration unavailable")
		return
	}
	defer c.close()
	bounded, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	transfers, e := c.observe(bounded, orders)
	if e != nil {
		log.Task.Error("HTTPS observer scan: ", e)
		return
	}
	if len(transfers) > 0 {
		transferQueue.In <- transfers
	}
}
func (c *tronHTTPSClient) observe(ctx context.Context, orders []model.Order) ([]transfer, error) {
	min := orders[0].CreatedAt.UnixMilli()
	for _, o := range orders {
		if o.CreatedAt.UnixMilli() < min {
			min = o.CreatedAt.UnixMilli()
		}
	}
	params := url.Values{"only_to": {"true"}, "only_confirmed": {"true"}, "contract_address": {httpsObserverContract}, "min_timestamp": {fmt.Sprint(min)}, "limit": {"200"}, "order_by": {"block_timestamp,asc"}}
	seen := map[string]bool{}
	var transfers []transfer
	for page := 0; page < 5; page++ {
		var r struct {
			Success bool `json:"success"`
			Data    []struct {
				ID string `json:"transaction_id"`
			} `json:"data"`
			Meta struct {
				Fingerprint string `json:"fingerprint"`
			} `json:"meta"`
		}
		if e := c.read(ctx, "GET", "/v1/accounts/"+httpsObserverRecipient+"/transactions/trc20?"+params.Encode(), nil, &r); e != nil {
			return nil, e
		}
		if !r.Success || len(r.Data) > 200 {
			return nil, errors.New("HTTPS_OBSERVER_INDEX_INVALID")
		}
		for _, hint := range r.Data {
			if seen[hint.ID] {
				continue
			}
			seen[hint.ID] = true
			id, e := hex.DecodeString(hint.ID)
			if e != nil || len(id) != 32 {
				return nil, errors.New("HTTPS_OBSERVER_INDEX_TX_INVALID")
			}
			info, e := c.GetTransactionInfoById(ctx, &api.BytesMessage{Value: id})
			if e != nil {
				return nil, e
			}
			b, e := c.GetBlockByNum2(ctx, &api.NumberMessage{Num: info.BlockNumber})
			if e != nil {
				return nil, e
			}
			stamp := b.GetBlockHeader().GetRawData().GetTimestamp()
			if stamp != info.BlockTimeStamp {
				return nil, errors.New("HTTPS_OBSERVER_TIMESTAMP_MISMATCH")
			}
			candidates := tr.parseTrc20ReceiptLogs(info, hint.ID, time.UnixMilli(stamp), int(info.BlockNumber))
			for _, t := range candidates {
				if t.RecvAddress != httpsObserverRecipient || t.TradeType != model.UsdtTrc20 {
					continue
				}
				for _, o := range orders {
					if stamp >= o.CreatedAt.UnixMilli() && stamp < o.ExpiredAt.UnixMilli() && t.Amount.String() == o.Amount {
						transfers = append(transfers, t)
						break
					}
				}
			}
		}
		if len(r.Data) < 200 || r.Meta.Fingerprint == "" {
			return transfers, nil
		}
		params.Set("fingerprint", r.Meta.Fingerprint)
	}
	return nil, errors.New("HTTPS_OBSERVER_PAGINATION_LIMIT")
}
