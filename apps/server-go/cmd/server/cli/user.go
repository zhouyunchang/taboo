package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	orgsvc "github.com/zhouyunchang/taboo/apps/server-go/internal/org"
)

// runUserCmd：本地账号运维（注册关闭时建号、停用/启用测试或离职账号）。
//
//	taboo-server user status  EMAIL
//	taboo-server user disable EMAIL
//	taboo-server user enable  EMAIL
//	taboo-server user create  EMAIL [--name NAME] [--disabled]   # 密码从 stdin 第一行读取
//
// 输出单行 JSON，便于脚本处理；从不回显密码。
const userUsage = "usage: taboo-server user status|disable|enable EMAIL | user create EMAIL [--name NAME] [--disabled] (password on stdin)"

func userEmailArgs() cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 {
			return exitf(2, "%s", userUsage)
		}
		return nil
	}
}

func newUserCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "user",
		Short: "Local account admin (status / enable / disable / create)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return exitf(2, "%s", userUsage)
		},
	}
	cmd.AddCommand(
		newUserStatusCmd(),
		newUserDisableCmd(),
		newUserEnableCmd(),
		newUserCreateCmd(),
	)
	return cmd
}

func newUserStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status EMAIL",
		Short: "Show local user status",
		Args:  userEmailArgs(),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withUserDB(func(rt *runtime) error {
				email := args[0]
				st, err := orgsvc.AdminUserStatus(rt.db, email)
				if err != nil {
					return userFail("status", err)
				}
				return userOut(map[string]any{"email": email, "status": st})
			})
		},
	}
}

func newUserDisableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "disable EMAIL",
		Short: "Disable a local user",
		Args:  userEmailArgs(),
		RunE: func(cmd *cobra.Command, args []string) error {
			return setUserStatus("disable", args[0], "disabled")
		},
	}
}

func newUserEnableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "enable EMAIL",
		Short: "Enable a local user",
		Args:  userEmailArgs(),
		RunE: func(cmd *cobra.Command, args []string) error {
			return setUserStatus("enable", args[0], "active")
		},
	}
}

func newUserCreateCmd() *cobra.Command {
	var name string
	var disabled bool
	cmd := &cobra.Command{
		Use:   "create EMAIL",
		Short: "Create a local user (password on stdin)",
		Args:  userEmailArgs(),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withUserDB(func(rt *runtime) error {
				email := args[0]
				status := "active"
				if disabled {
					status = "disabled"
				}
				line, err := bufio.NewReader(os.Stdin).ReadString('\n')
				if err != nil && line == "" {
					return userFail("create", errors.New("password must be provided on stdin"))
				}
				u, err := orgsvc.AdminCreateUser(rt.db, rt.masterKey, email, name, strings.TrimRight(line, "\r\n"), status)
				if err != nil {
					return userFail("create", err)
				}
				return userOut(map[string]any{"email": email, "status": status, "user_id": u.UserID, "org_slug": u.Slug})
			})
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "display name")
	cmd.Flags().BoolVar(&disabled, "disabled", false, "create as disabled")
	return cmd
}

func setUserStatus(cmdName, email, status string) error {
	return withUserDB(func(rt *runtime) error {
		changed, err := orgsvc.AdminSetUserStatus(rt.db, email, status)
		if err != nil {
			return userFail(cmdName, err)
		}
		return userOut(map[string]any{"email": email, "status": status, "changed": changed})
	})
}

func withUserDB(fn func(*runtime) error) error {
	initJSONLogger()
	rt, err := loadRuntime(true)
	if err != nil {
		return err
	}
	defer rt.close()
	return fn(rt)
}

func userOut(v map[string]any) error {
	b, _ := json.Marshal(v)
	fmt.Println(string(b))
	return nil
}

func userFail(cmd string, err error) error {
	msg := fmt.Sprintf("[taboo] user %s: %v", cmd, err)
	code := 1
	if errors.Is(err, orgsvc.ErrUserNotFound) {
		code = 3
	}
	fmt.Fprintln(os.Stderr, msg)
	return &exitError{code: code, msg: msg, printed: true}
}
