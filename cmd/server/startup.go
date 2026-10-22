package main

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
)

// loadStartupConfig fails closed if a missing config cannot be created or reloaded.
// create is injectable so write failures can be tested on Windows and Unix alike.
func loadStartupConfig(path string, create func(string) (string, error)) (*Config, error) {
	cfg, err := Load(path)
	if !errors.Is(err, fs.ErrNotExist) {
		return cfg, err
	}
	if _, err := create(path); err != nil {
		return nil, fmt.Errorf("create config: %w", err)
	}
	log.Printf("config %s 不存在，已生成推荐配置；请从该文件读取 api_key（密钥不写入日志）", path)
	return Load(path)
}
