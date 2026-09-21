package store

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Backup 将 taboo.db VACUUM 到 destDir，并复制 master.key（0600）。恢复仍是停机替换数据目录。
func Backup(dataDir, destDir string) error {
	if destDir == "" {
		return fmt.Errorf("backup dest dir required")
	}
	if err := os.MkdirAll(destDir, 0o700); err != nil {
		return err
	}
	srcDB := filepath.Join(dataDir, "taboo.db")
	if _, err := os.Stat(srcDB); err != nil {
		return fmt.Errorf("source db: %w", err)
	}
	destDB := filepath.Join(destDir, "taboo.db")
	_ = os.Remove(destDB)

	db, err := Open(dataDir)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.Exec(`VACUUM INTO ?`, destDB); err != nil {
		return fmt.Errorf("VACUUM INTO: %w", err)
	}

	srcKey := filepath.Join(dataDir, "master.key")
	if raw, err := os.ReadFile(srcKey); err == nil {
		destKey := filepath.Join(destDir, "master.key")
		if err := os.WriteFile(destKey, raw, 0o600); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}

// CopyFile 小文件拷贝（测试/工具用）
func CopyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
