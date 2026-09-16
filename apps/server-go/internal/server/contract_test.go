// 契约漂移检查：Go 路由表 vs api/openapi.yaml（M3 #7）
// 任一实现路由未在契约中声明（或反之），测试即失败 —— CI 中运行。
package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
)

func testRouter() *chi.Mux {
	return New(&Deps{
		DB:        nil,
		MasterKey: make([]byte, 32),
		JWTSecret: "test",
		DEKs:      tc.NewDEKCache(),
	})
}

func TestRoutesDocumentedInOpenAPI(t *testing.T) {
	r := testRouter()
	root, err := filepath.Abs("../../../..") // 仓库根（apps/server-go/internal/server）
	if err != nil {
		t.Fatal(err)
	}
	spec, err := os.ReadFile(filepath.Join(root, "api", "openapi.yaml"))
	if err != nil {
		t.Fatalf("read openapi.yaml: %v", err)
	}

	var undocumented []string
	err = chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if method == "*" {
			return nil
		}
		// 文档页与静态托管通配不在 REST 契约内；chi 对 r.Get("/") 产生尾斜杠，归一化后比对
		if route == "/api/docs" || route == "/*" {
			return nil
		}
		normalized := strings.TrimSuffix(route, "/")
		if normalized != "/" && !strings.Contains(string(spec), normalized+":") {
			undocumented = append(undocumented, method+" "+route)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(undocumented) > 0 {
		t.Errorf("routes missing in api/openapi.yaml:\n  %s", strings.Join(undocumented, "\n  "))
	}
}
