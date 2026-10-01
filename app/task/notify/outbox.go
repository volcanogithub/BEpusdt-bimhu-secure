package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/cast"
	"github.com/v03413/bepusdt/app/model"
	"github.com/v03413/bepusdt/app/security"
	"github.com/v03413/bepusdt/app/utils"
)

func ProcessOne(ctx context.Context, worker string, client *http.Client, now time.Time, lease time.Duration) error {
	if lease <= 0 {
		lease = 30 * time.Second
	}
	delivery, err := model.ClaimNotification(worker, now, lease)
	if err != nil {
		return err
	}
	var order model.Order
	if err = model.Db.First(&order, delivery.OrderID).Error; err != nil {
		_ = model.RetryNotificationAfter(delivery.ID, worker, err.Error(), time.Minute)
		return err
	}
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}

	deliveryCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	leaseErr := make(chan error, 1)
	interval := lease / 3
	if interval < time.Millisecond {
		interval = time.Millisecond
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if renewErr := model.ExtendNotificationLease(delivery.ID, worker, lease); renewErr != nil {
					leaseErr <- renewErr
					cancel()
					return
				}
			}
		}
	}()
	err = deliverStableEvent(deliveryCtx, client, order, delivery.EventID)
	close(done)
	cancel()
	select {
	case renewErr := <-leaseErr:
		return renewErr
	default:
	}
	if err != nil {
		maxAttempts := cast.ToInt(model.GetC(model.NotifyMaxRetry))
		if maxAttempts <= 0 {
			maxAttempts = 10
		}
		if delivery.AttemptCount >= maxAttempts {
			if failErr := model.FailNotification(delivery.ID, worker, err.Error()); failErr != nil {
				return failErr
			}
			return err
		}
		shift := delivery.AttemptCount
		if shift < 1 {
			shift = 1
		}
		if shift > 6 {
			shift = 6
		}
		delay := time.Minute * time.Duration(1<<shift)
		if retryErr := model.RetryNotificationAfter(delivery.ID, worker, err.Error(), delay); retryErr != nil {
			return retryErr
		}
		return err
	}
	return model.CompleteNotification(delivery.ID, worker)
}

func deliverStableEvent(ctx context.Context, client *http.Client, order model.Order, eventID string) error {
	client = security.Client(client)
	if order.ApiType == model.OrderApiTypeEpay {
		notifyURL := fmt.Sprintf("%s?%s&event_id=%s", order.NotifyUrl, order.BuildNotifyParams(), url.QueryEscape(eventID))
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, notifyURL, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Idempotency-Key", eventID)
		req.Header.Set("X-BEpusdt-Event-ID", eventID)
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("notification response status %d", resp.StatusCode)
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		ack := strings.ToLower(strings.TrimSpace(string(body)))
		if !strings.Contains(ack, "success") && !strings.Contains(ack, "ok") {
			return fmt.Errorf("epay notification was not acknowledged")
		}
		return nil
	}
	body := EpNotify{EventID: eventID, TradeId: order.TradeId, OrderId: order.OrderId,
		Amount: cast.ToFloat64(order.Money), ActualAmount: order.Amount, Token: order.Address,
		BlockTransactionId: order.RefHash, Status: order.Status}
	unsigned, err := json.Marshal(body)
	if err != nil {
		return err
	}
	var data map[string]interface{}
	if err = json.Unmarshal(unsigned, &data); err != nil {
		return err
	}
	body.Signature = utils.EpusdtSign(data, model.AuthToken())
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, order.NotifyUrl, strings.NewReader(string(payload)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", eventID)
	req.Header.Set("X-BEpusdt-Event-ID", eventID)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("notification response status %d", resp.StatusCode)
	}
	return nil
}
