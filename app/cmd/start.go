package cmd

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"context"

	"github.com/v03413/bepusdt/app"
	"github.com/v03413/bepusdt/app/deployment"
	"github.com/v03413/bepusdt/app/log"
	"github.com/v03413/bepusdt/app/model"
	"github.com/v03413/bepusdt/app/notifier"
	"github.com/v03413/bepusdt/app/router"
	"github.com/v03413/bepusdt/app/task"
)

import (
	"github.com/urfave/cli/v3"
)

var Start = &cli.Command{
	Name:  "start",
	Usage: "启动收款网关",
	Flags: []cli.Flag{SQLiteFlag, PostgresDSNFlag, LogFlag, ListenFlag},
	Before: func(ctx context.Context, c *cli.Command) (context.Context, error) {
		postgres := c.String("postgres")
		sqlite := c.String("sqlite")
		production := os.Getenv("BEPUSDT_PRODUCTION")
		if err := deployment.ValidateListener(production, os.Getenv("BEPUSDT_PRIVATE_NETWORK"), c.String("listen")); err != nil {
			return ctx, err
		}
		var err error
		postgres, err = deployment.DatabaseDSN(postgres, os.Getenv("POSTGRESQL_DSN_FILE"))
		if err != nil {
			return ctx, err
		}
		log.RegisterSecrets(postgres)
		if production == "1" {
			if _, err := os.Lstat(".env"); err == nil {
				if _, err := deployment.ReadPrivateFile(".env"); err != nil {
					return ctx, err
				}
			}
		}
		if err := model.Init(sqlite, postgres); err != nil {
			return ctx, fmt.Errorf("数据库初始化失败 %w", err)
		}

		if err := log.Init(c.String("log")); err != nil {
			return ctx, fmt.Errorf("日志初始化失败 %w", err)
		}
		if production == "1" {
			values := make(map[string]string)
			for _, key := range []model.ConfKey{model.AdminUsername, model.AdminPassword, model.AdminSecret, model.ApiAuthToken} {
				values[string(key)] = model.GetK(key)
			}
			if err := deployment.ValidateSecrets(values); err != nil {
				return ctx, err
			}
			if postgres == "" {
				for _, path := range []string{sqlite, sqlite + "-wal", sqlite + "-shm"} {
					if _, err := os.Lstat(path); err == nil {
						if err := deployment.ProtectFile(path); err != nil {
							return ctx, err
						}
					}
				}
			}
		}

		return ctx, task.Init()
	},
	After: func(ctx context.Context, c *cli.Command) error {
		log.Close()
		model.Close()

		return nil
	},
	Action: start,
}

func start(ctx context.Context, cmd *cli.Command) error {
	var listen = cmd.String("listen")
	listener, err := net.Listen("tcp", listen)
	if err != nil {
		return fmt.Errorf("backend listen failed: %w", err)
	}
	// 开始任务调度
	task.Start(ctx)

	// 启动 Web 服务器
	var srv = &http.Server{Addr: listen, Handler: router.Handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}

	log.Info("web server Start listen", listen)

	go func() {
		if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("web server error", err)
		}
	}()

	// 关闭 Web 服务器
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := srv.Shutdown(shutdown); err != nil {
			log.Error("Web shutdown Error", err)
			return
		}

		log.Info("web shutdown success.")
	}()

	notifier.Welcome()

	fmt.Println(fmt.Sprintf("日志保存路径：%s", log.GetPath()))
	fmt.Println(fmt.Sprintf("BEpusdt 启动成功(%s)，当前版本：%s", listen, app.Version))

	// 等待中断信号
	var signals = make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	<-signals

	runtime.GC()

	return nil
}
