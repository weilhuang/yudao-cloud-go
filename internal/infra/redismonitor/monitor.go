// Package redismonitor 提供管理后台的 Redis 监控视图，不读取任何业务 Key 的内容。
package redismonitor

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/redis/go-redis/v9"
)

// Reader 只暴露 INFO 和 DBSIZE，方便把监控限定在只读命令上。
type Reader interface {
	Info(ctx context.Context, section string) (string, error)
	DBSize(ctx context.Context) (int64, error)
}

// RedisReader 使用进程已经连接的逻辑库；DBSIZE 因而与业务 Redis DB 一致。
type RedisReader struct{ Client *redis.Client }

func (r RedisReader) Info(ctx context.Context, section string) (string, error) {
	if section == "" {
		return r.Client.Info(ctx).Result()
	}
	return r.Client.Info(ctx, section).Result()
}

func (r RedisReader) DBSize(ctx context.Context) (int64, error) {
	return r.Client.DBSize(ctx).Result()
}

// MonitorInfo 对齐 Java RedisMonitorRespVO 与固定 Vben 页面的 JSON 字段。
type MonitorInfo struct {
	Info         map[string]string `json:"info"`
	DBSize       int64             `json:"dbSize"`
	CommandStats []CommandStat     `json:"commandStats"`
}

type CommandStat struct {
	Command string `json:"command"`
	Calls   int64  `json:"calls"`
	Usec    int64  `json:"usec"`
}

// Read 依次读取 Redis 全局信息、当前逻辑库大小和命令统计。
// Redis INFO 本来就是服务器级数据，不能误认为只包含当前租户的统计。
func Read(ctx context.Context, reader Reader) (*MonitorInfo, error) {
	rawInfo, err := reader.Info(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("读取 Redis INFO: %w", err)
	}
	dbSize, err := reader.DBSize(ctx)
	if err != nil {
		return nil, fmt.Errorf("读取 Redis DBSIZE: %w", err)
	}
	rawStats, err := reader.Info(ctx, "commandstats")
	if err != nil {
		return nil, fmt.Errorf("读取 Redis 命令统计: %w", err)
	}
	stats, err := parseCommandStats(rawStats)
	if err != nil {
		return nil, err
	}
	return &MonitorInfo{Info: parseInfo(rawInfo), DBSize: dbSize, CommandStats: stats}, nil
}

func parseInfo(raw string) map[string]string {
	return parseProperties(raw, true)
}

func parseProperties(raw string, redact bool) map[string]string {
	info := make(map[string]string)
	for _, line := range strings.Split(raw, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok || key == "" || strings.HasPrefix(key, "#") || redact && sensitiveInfoKey(key) {
			continue
		}
		info[key] = value
	}
	return info
}

// INFO 通常没有凭据；仍过滤可能由 Redis 扩展或新版本加入的敏感字段。
// 运行路径和复制主机等机器标识也不应从监控页暴露给浏览器。
func sensitiveInfoKey(key string) bool {
	key = strings.ToLower(key)
	switch key {
	case "config_file", "executable", "run_id", "master_host", "master_user", "os":
		return true
	}
	for _, fragment := range []string{"pass", "secret", "token", "credential", "auth", "private", "acl"} {
		if strings.Contains(key, fragment) {
			return true
		}
	}
	return false
}

func parseCommandStats(raw string) ([]CommandStat, error) {
	// AUTH/ACL 命令的调用次数并非凭据；这里保留 Java 展示的全部命令统计。
	properties := parseProperties(raw, false)
	stats := make([]CommandStat, 0, len(properties))
	for key, value := range properties {
		if !strings.HasPrefix(key, "cmdstat_") {
			continue
		}
		fields := make(map[string]string)
		for _, item := range strings.Split(value, ",") {
			name, number, ok := strings.Cut(item, "=")
			if ok {
				fields[name] = number
			}
		}
		calls, callsErr := strconv.ParseInt(fields["calls"], 10, 64)
		usec, usecErr := strconv.ParseInt(fields["usec"], 10, 64)
		if callsErr != nil || usecErr != nil || calls < 0 || usec < 0 {
			return nil, fmt.Errorf("Redis 命令统计格式不正确: %s", key)
		}
		stats = append(stats, CommandStat{Command: strings.TrimPrefix(key, "cmdstat_"), Calls: calls, Usec: usec})
	}
	sort.Slice(stats, func(i, j int) bool { return stats[i].Command < stats[j].Command })
	return stats, nil
}
