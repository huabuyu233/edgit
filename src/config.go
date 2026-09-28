// config.go 配置与缓存：~/.edgit/config.json（镜像表）+ cache.json（命中缓存，10 分钟有效）。
package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// Config 配置文件结构。
type Config struct {
	Version int      `json:"version"`
	Mirrors []Mirror `json:"mirrors"`
}

// configVersion 配置结构版本。v2 起内置镜像含 Docker 加速站。
const configVersion = 2

// cacheData 命中缓存：最近一次克隆成功的镜像及时间。
type cacheData struct {
	HitID string    `json:"hit_id"`
	HitAt time.Time `json:"hit_at"`
}

const cacheTTL = 10 * time.Minute // 命中缓存有效期

// defaultConfig 内置默认配置（配置文件不存在或为空时使用）。
func defaultConfig() *Config {
	return &Config{Version: configVersion, Mirrors: builtinMirrors()}
}

// edgitDir 数据目录 ~/.edgit。
func edgitDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".edgit"), nil
}

// configPath 配置文件 ~/.edgit/config.json。
func configPath() (string, error) {
	dir, err := edgitDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// cachePath 命中缓存 ~/.edgit/cache.json。
func cachePath() (string, error) {
	dir, err := edgitDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "cache.json"), nil
}

// loadCacheHit 读取命中缓存；过期、镜像已删除或已禁用时返回 nil（视为未命中）。
func loadCacheHit(ms []Mirror) *Mirror {
	p, err := cachePath()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var c cacheData
	if err := json.Unmarshal(data, &c); err != nil {
		return nil
	}
	if time.Since(c.HitAt) > cacheTTL {
		return nil
	}
	m := findMirror(ms, c.HitID)
	if m == nil || !m.Enabled {
		return nil
	}
	return m
}

// saveCacheHit 记录本次克隆命中的镜像（写入缓存失败静默忽略）。
func saveCacheHit(m Mirror) {
	p, err := cachePath()
	if err != nil {
		return
	}
	data, err := json.MarshalIndent(cacheData{HitID: m.ID, HitAt: time.Now()}, "", "  ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	_ = os.WriteFile(p, data, 0o644)
}

// loadConfig 读取配置：文件不存在用默认配置；旧版本配置自动补入新增的内置镜像（版本迁移）。
func loadConfig() (*Config, error) {
	p, err := configPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return defaultConfig(), nil
		}
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	if len(c.Mirrors) == 0 {
		return defaultConfig(), nil
	}
	if c.Version < configVersion {
		for _, b := range builtinMirrors() {
			if b.DockerTemplate == "" || findMirror(c.Mirrors, b.ID) != nil {
				continue
			}
			c.Mirrors = append(c.Mirrors, b)
		}
		c.Version = configVersion
	}
	return &c, nil
}

// save 写回配置文件（自动创建 ~/.edgit 目录）。
func (c *Config) save() error {
	p, err := configPath()
	if err != nil {
		return err
	}
	if p == "" {
		return errors.New("无法确定配置目录")
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, append(data, '\n'), 0o644)
}

// saveConfig 保存配置，失败即报错退出。
func saveConfig(c *Config) {
	if err := c.save(); err != nil {
		fatalf("保存配置失败: %v", err)
	}
}
