// Package rpc 按服务名发现实例，再发 HTTP。业务包不直接依赖 Nacos SDK。
package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/weilhuang/yudao-cloud-go/internal/platform/config"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/nacos"
)

// CheckedToken 是 /rpc-api/system/oauth2/token/check 的数据。
type CheckedToken struct {
	UserID   int64
	UserType int
	TenantID int64
}

// CheckToken 发现 system-server 并校验访问令牌。
// tag 非空且没有匹配实例时返回错误，不把请求打到其他实例。
func CheckToken(ctx context.Context, cfg config.Nacos, tag, accessToken string) (*CheckedToken, error) {
	instance, err := Pick(cfg, "system-server", tag)
	if err != nil {
		return nil, err
	}
	endpoint := fmt.Sprintf("http://%s:%d/rpc-api/system/oauth2/token/check?accessToken=%s",
		instance.IP, instance.Port, url.QueryEscape(accessToken))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var wrapped struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			UserID   int64 `json:"userId"`
			UserType int   `json:"userType"`
			TenantID int64 `json:"tenantId"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &wrapped); err != nil {
		return nil, err
	}
	if wrapped.Code != 0 {
		return nil, fmt.Errorf("校验令牌失败: %s", wrapped.Msg)
	}
	return &CheckedToken{UserID: wrapped.Data.UserID, UserType: wrapped.Data.UserType, TenantID: wrapped.Data.TenantID}, nil
}

// Pick 按 tag 选择一个实例。规则与 Java 灰度负载的 tag 过滤相同，但 tag 对不上时不回退到全部实例。
func Pick(cfg config.Nacos, service, tag string) (nacos.Instance, error) {
	list, err := nacos.SelectHealthy(cfg, service)
	if err != nil {
		return nacos.Instance{}, err
	}
	chosen := filterTag(list, tag)
	if len(chosen) == 0 {
		return nacos.Instance{}, fmt.Errorf("没有 tag=%q 的 %s 实例", tag, service)
	}
	return chosen[0], nil
}

func filterTag(list []nacos.Instance, tag string) []nacos.Instance {
	if tag == "" {
		var plain []nacos.Instance
		for _, item := range list {
			if item.Metadata["tag"] == "" {
				plain = append(plain, item)
			}
		}
		if len(plain) == 0 {
			return list
		}
		return plain
	}
	var matched []nacos.Instance
	for _, item := range list {
		if item.Metadata["tag"] == tag {
			matched = append(matched, item)
		}
	}
	return matched
}
