package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

type Config struct {
	Mirrors []Mirror `json:"mirrors"`
}

type cacheData struct {
	HitID string    `json:"hit_id"`
	HitAt time.Time `json:"hit_at"`
}

const cacheTTL = 10 * time.Minute

func defaultConfig() *Config {
	return &Config{Mirrors: builtinMirrors()}
}

func edgitDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".edgit"), nil
}

func configPath() (string, error) {
	dir, err := edgitDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

func cachePath() (string, error) {
	dir, err := edgitDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "cache.json"), nil
}

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
	return &c, nil
}

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

func saveConfig(c *Config) {
	if err := c.save(); err != nil {
		fatalf("保存配置失败: %v", err)
	}
}
