package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"
	"github.com/v03413/bepusdt/app/credential"
	"github.com/v03413/bepusdt/app/model"
	"github.com/v03413/bepusdt/app/task"
	"golang.org/x/crypto/bcrypt"
)

var Reset = &cli.Command{
	Name:  "reset",
	Usage: "忘记密码时，此命令可重置账号密码登录入口",
	Flags: []cli.Flag{SQLiteFlag, PostgresDSNFlag},
	Before: func(ctx context.Context, c *cli.Command) (context.Context, error) {
		sqlite := c.String("sqlite")
		postgres := c.String("postgres")
		if err := model.Init(sqlite, postgres); err != nil {
			return ctx, fmt.Errorf("数据库初始化失败 %w", err)
		}

		return ctx, task.Init()
	},
	After: func(ctx context.Context, c *cli.Command) error {
		model.Close()

		return nil
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		credentials, err := credential.Generate()
		if err != nil {
			return fmt.Errorf("generate reset credentials: %w", err)
		}
		encrypt, err := bcrypt.GenerateFromPassword([]byte(credentials.Password), bcrypt.DefaultCost)
		if err != nil {
			return fmt.Errorf("hash reset password: %w", err)
		}

		handoff := credentials
		handoff.AdminSecret = ""
		handoff.APIToken = ""
		path, err := credential.WriteOneTimeFile(handoff)
		if err != nil {
			return err
		}
		if err := model.SetSecretValues(map[model.ConfKey]string{
			model.AdminSecure:   credentials.AdminPath,
			model.AdminUsername: credentials.Username,
			model.AdminPassword: string(encrypt),
		}); err != nil {
			_ = os.Remove(path)
			return fmt.Errorf("store reset credentials: %w", err)
		}
		fmt.Println("Initial credential generated; plaintext values were not written to stdout or logs.")
		fmt.Printf("Retrieve the one-time credential file securely, then delete it: %s\n", path)

		return nil
	},
}
