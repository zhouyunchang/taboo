// Package buildinfo 暴露构建时注入的版本信息（-ldflags -X），
// 未注入时回退到 Go 工具链记录的 VCS 信息，便于定位线上运行的是哪个提交。
package buildinfo

import (
	"runtime/debug"
	"sync"
)

const pkg = "github.com/zhouyunchang/taboo/apps/server-go/internal/buildinfo"

// 由 -ldflags "-X <pkg>.version=... -X <pkg>.commit=... -X <pkg>.buildTime=..." 注入。
var (
	version   = ""
	commit    = ""
	buildTime = ""
)

// Info 是对外公开的构建信息（不含任何敏感数据）。
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildTime string `json:"build_time,omitempty"`
}

var (
	once sync.Once
	info Info
)

// Get 返回构建信息；Version 缺省为 "dev"，Commit 缺省为 "unknown"。
func Get() Info {
	once.Do(func() {
		info = Info{Version: version, Commit: commit, BuildTime: buildTime}
		if bi, ok := debug.ReadBuildInfo(); ok {
			dirty := false
			for _, s := range bi.Settings {
				switch s.Key {
				case "vcs.revision":
					if info.Commit == "" && len(s.Value) >= 7 {
						info.Commit = s.Value[:7]
					}
				case "vcs.time":
					if info.BuildTime == "" {
						info.BuildTime = s.Value
					}
				case "vcs.modified":
					dirty = s.Value == "true"
				}
			}
			if commit == "" && dirty && info.Commit != "" {
				info.Commit += "-dirty"
			}
		}
		if info.Version == "" {
			info.Version = "dev"
		}
		if info.Commit == "" {
			info.Commit = "unknown"
		}
	})
	return info
}

// String 形如 "0.2.0 (abc1234, 2026-10-31T12:00:00Z)"。
func (i Info) String() string {
	s := i.Version + " (" + i.Commit
	if i.BuildTime != "" {
		s += ", " + i.BuildTime
	}
	return s + ")"
}

// LDFlagsPackage 供构建脚本拼接 -X 参数时参考。
const LDFlagsPackage = pkg
