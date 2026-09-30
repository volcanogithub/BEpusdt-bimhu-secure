package task

import (
	"encoding/hex"
	"math/big"
	"testing"
	"time"

	"github.com/v03413/tronprotocol/core"
)

func TestB1TRC20ReceiptLogIndexDistinguishesMultipleEvents(t *testing.T) {
	topic, err := hex.DecodeString(trc20TransferTopic)
	if err != nil {
		t.Fatal(err)
	}
	addressTopic := func(last byte) []byte {
		value := make([]byte, 32)
		value[31] = last
		return value
	}
	amount := func(value int64) []byte {
		return new(big.Int).SetInt64(value).FillBytes(make([]byte, 32))
	}
	log := func(value int64, from, to byte) *core.TransactionInfo_Log {
		return &core.TransactionInfo_Log{
			Address: usdtTrc20ContractAddress,
			Topics:  [][]byte{topic, addressTopic(from), addressTopic(to)},
			Data:    amount(value),
		}
	}

	info := &core.TransactionInfo{Log: []*core.TransactionInfo_Log{
		log(1_000_000, 1, 2),
		log(2_000_000, 3, 4),
	}}
	events := tr.parseTrc20ReceiptLogs(info, "same-tx", time.Unix(1, 0), 100)
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	if events[0].TxHash != events[1].TxHash || events[0].EventIndex != 0 || events[1].EventIndex != 1 {
		t.Fatalf("receipt log identities = (%s,%d), (%s,%d)", events[0].TxHash, events[0].EventIndex, events[1].TxHash, events[1].EventIndex)
	}
	if events[0].Amount.Equal(events[1].Amount) {
		t.Fatal("distinct receipt logs lost their individual amounts")
	}
}
