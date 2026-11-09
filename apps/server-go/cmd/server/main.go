// taboo API Server 入口（Go 版）—— 单体优先：API + 前端静态文件同端口
package main

import (
	"embed"

	"github.com/zhouyunchang/taboo/apps/server-go/cmd/server/cli"
)

//go:embed all:web
var webFS embed.FS

func main() {
	cli.Execute(cli.Options{WebFS: webFS})
}
