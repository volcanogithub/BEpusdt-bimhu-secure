package main

import (
	"context"
	"fmt"
	"os"

	"github.com/joho/godotenv"
	"github.com/urfave/cli/v3"
	"github.com/v03413/bepusdt/app/cmd"
	"github.com/v03413/bepusdt/app/conf"
	"github.com/v03413/bepusdt/app/deployment"
	"github.com/v03413/bepusdt/app/log"
)

func init() {
	deployment.RestrictCreation()
	// 不推荐引导小白参与修改各种配置文件
	_ = godotenv.Load()
}

func main() {
	c := &cli.Command{
		Name:  "BEpusdt",
		Usage: conf.Desc,
		Commands: []*cli.Command{
			cmd.Start,
			cmd.Version,
			cmd.Reset,
		},
	}
	if err := c.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintln(os.Stderr, log.Redact(err.Error()))
		os.Exit(1)
	}
}
