// 密钥定位符：归属/项目/目录。密钥名不属于这段路径。
package locator

import (
	"strings"

	"github.com/zhouyunchang/taboo/apps/server-go/internal/pathcode"
)

type Location struct {
	Owner   string
	Project string
	Folder  string
}

// Parse 解析归属/项目/目录。根目录写作 归属/项目。
func Parse(raw string) (Location, error) {
	raw = strings.Trim(strings.TrimSpace(raw), "/")
	parts := strings.Split(raw, "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return Location{}, errInvalid
	}
	if !pathcode.Valid(parts[0]) || !pathcode.ValidToken(parts[1]) {
		return Location{}, errInvalid
	}
	folder := "/"
	if len(parts) > 2 {
		for _, part := range parts[2:] {
			if !pathcode.ValidToken(part) {
				return Location{}, errInvalid
			}
		}
		folder = "/" + strings.Join(parts[2:], "/") + "/"
	}
	return Location{Owner: parts[0], Project: parts[1], Folder: folder}, nil
}

type invalidError struct{}

func (invalidError) Error() string { return "invalid locator" }

var errInvalid invalidError

func IsInvalid(err error) bool {
	_, ok := err.(invalidError)
	return ok
}
