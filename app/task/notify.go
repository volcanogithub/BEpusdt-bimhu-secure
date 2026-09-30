package task

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/v03413/bepusdt/app/log"
	"github.com/v03413/bepusdt/app/model"
	"github.com/v03413/bepusdt/app/task/notify"
)

func init() {
	Register(Task{Duration: time.Second * 3, Callback: notifyRetry})
	Register(Task{Duration: time.Second * 30, Callback: notifyRoll})
}

func notifyRetry(ctx context.Context) {
	worker := fmt.Sprintf("%s-%d", durableNotifyHostname(), os.Getpid())
	for i := 0; i < 10; i++ {
		err := notify.ProcessOne(ctx, worker, nil, time.Now(), 30*time.Second)
		if errors.Is(err, model.ErrNoNotificationDue) {
			return
		}
		if err != nil {
			log.Task.Warn("durable notification delivery failed", err)
			return
		}
	}
}

func durableNotifyHostname() string {
	h, _ := os.Hostname()
	return h
}

func notifyRoll(context.Context) {
	for _, o := range model.GetOrderByStatus(model.OrderStatusWaiting) {
		notify.Bepusdt(o)
	}
}
