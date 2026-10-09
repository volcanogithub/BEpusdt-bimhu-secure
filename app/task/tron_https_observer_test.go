package task

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"github.com/btcsuite/btcd/btcutil/base58"
	"github.com/v03413/bepusdt/app/model"
	"github.com/v03413/tronprotocol/api"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHTTPSObserverCanonicalTimestampAndReceiptPosition(t *testing.T) {
	tx := strings.Repeat("a", 64)
	stamp := int64(1791555000123)
	addr := base58.Decode(httpsObserverRecipient)[1:21]
	recipientTopic := strings.Repeat("0", 24) + hex.EncodeToString(addr)
	receipt := observerReceipt{ID: tx, BlockNumber: 100, BlockTime: stamp}
	receipt.Receipt.Result = "SUCCESS"
	receipt.Logs = append(receipt.Logs, struct {
		Address string   `json:"address"`
		Topics  []string `json:"topics"`
		Data    string   `json:"data"`
	}{Address: strings.Repeat("0", 40), Topics: nil, Data: ""})
	receipt.Logs = append(receipt.Logs, struct {
		Address string   `json:"address"`
		Topics  []string `json:"topics"`
		Data    string   `json:"data"`
	}{Address: hex.EncodeToString(usdtTrc20ContractAddress[1:]), Topics: []string{trc20TransferTopic, strings.Repeat("0", 64), recipientTopic}, Data: strings.Repeat("0", 58) + "0f4240"})
	calls := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("TRON-PRO-API-KEY") != "isolated-fixture-key" {
			t.Error("missing secure provider credential")
		}
		if r.TLS == nil {
			t.Error("unencrypted provider request")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/accounts/" + httpsObserverRecipient + "/transactions/trc20":
			if r.URL.Query().Get("contract_address") != httpsObserverContract {
				t.Error("wrong contract filter")
			}
			json.NewEncoder(w).Encode(map[string]any{"success": true, "data": []map[string]string{{"transaction_id": tx}}})
		case "/wallet/gettransactioninfobyid":
			json.NewEncoder(w).Encode(receipt)
		case "/wallet/getblockbynum":
			json.NewEncoder(w).Encode(map[string]any{"blockID": strings.Repeat("b", 64), "block_header": map[string]any{"raw_data": map[string]any{"number": 100, "timestamp": stamp}}})
		default:
			t.Error("unexpected route")
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	c := &tronHTTPSClient{base: server.URL, key: "isolated-fixture-key", http: server.Client()}
	order := model.Order{Amount: "1", Address: httpsObserverRecipient, TradeType: model.UsdtTrc20, ExpiredAt: time.UnixMilli(stamp + 1000)}
	created := model.Datetime(time.UnixMilli(stamp - 500))
	order.CreatedAt = &created
	transfers, e := c.observe(context.Background(), []model.Order{order})
	if e != nil {
		t.Fatal(e)
	}
	if len(transfers) != 1 || transfers[0].EventIndex != 1 || transfers[0].Timestamp.UnixMilli() != stamp || transfers[0].RecvAddress != httpsObserverRecipient || calls != 3 {
		t.Fatal("canonical milliseconds or ordered receipt index lost")
	}
	receipt.BlockTime = stamp - 1
	if _, e = c.observe(context.Background(), []model.Order{order}); e == nil {
		t.Fatal("receipt/block timestamp mismatch accepted")
	}
	receipt.BlockTime = stamp
	receipt.Receipt.Result = "FAILED"
	if _, e = c.observe(context.Background(), []model.Order{order}); e == nil {
		t.Fatal("failed receipt accepted")
	}
}
func TestHTTPSObserverPrivateOriginAndSanitizedFailures(t *testing.T) {
	dir := t.TempDir()
	key := filepath.Join(dir, "key")
	file := filepath.Join(dir, "config")
	os.WriteFile(key, []byte("isolated-key-secret-123456"), 0600)
	write := func(base string) {
		b, _ := json.Marshal(tronHTTPSConfig{BaseURL: base, APIKeyFile: key})
		os.WriteFile(file, b, 0600)
	}
	t.Setenv("BEPUSDT_TRON_HTTPS_CONFIG_FILE", file)
	write("https://api.trongrid.io")
	c, e := newTronHTTPSClient()
	if e != nil {
		t.Fatal(e)
	}
	c.close()
	for _, base := range []string{"http://api.trongrid.io", "https://nile.trongrid.io", "https://api.trongrid.io/credential", "https://external.invalid"} {
		write(base)
		if _, e = newTronHTTPSClient(); e == nil {
			t.Fatal("unsafe provider accepted")
		}
	}
	write("https://api.trongrid.io")
	os.Chmod(key, 0644)
	if _, e = newTronHTTPSClient(); e == nil {
		t.Fatal("nonprivate key accepted")
	}
	os.Chmod(key, 0600)
	link := filepath.Join(dir, "link")
	os.Symlink(key, link)
	if _, e = privateObserverFile(link); e == nil {
		t.Fatal("symlink secret accepted")
	}
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		w.Write([]byte("credential-response-secret"))
	}))
	defer s.Close()
	c = &tronHTTPSClient{base: s.URL, key: "isolated-key-secret-123456", http: s.Client()}
	_, e = c.GetNowBlock2(context.Background(), &api.EmptyMessage{})
	if e == nil || strings.Contains(e.Error(), s.URL) || strings.Contains(e.Error(), "secret") {
		t.Fatal("provider error leaked endpoint or body")
	}
}
