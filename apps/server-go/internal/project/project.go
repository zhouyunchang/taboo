// 项目作用域上下文：secret / folder 等服务共用（避免相互 import cycle）
package project

import (
	"context"
	"net/http"
)

type ctxKey string

const Key ctxKey = "taboo.project"

type Ctx struct {
	ID    string
	OrgID string
	Slug  string
}

func With(parent context.Context, c Ctx) context.Context {
	return context.WithValue(parent, Key, c)
}

func Of(r *http.Request) Ctx {
	p, _ := r.Context().Value(Key).(Ctx)
	return p
}
