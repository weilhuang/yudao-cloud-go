// Package config 提供参数配置。新建的都是自定义类型，系统内置类型不能删。
package config

const (
	typeSystem = 1
	typeCustom = 2
)

// Item 对应 infra_config。JSON 的 key 对应列 config_key。
type Item struct {
	ID         int64  `json:"id"`
	Category   string `json:"category"`
	Name       string `json:"name"`
	Key        string `json:"key"`
	Value      string `json:"value"`
	Type       int    `json:"type"`
	Visible    bool   `json:"visible"`
	Remark     string `json:"remark"`
	CreateTime int64  `json:"createTime,omitempty"`
}

// Page 是管理后台分页。
type Page[T any] struct {
	List  []T   `json:"list"`
	Total int64 `json:"total"`
}

// Error 是参数配置的业务错误。
type Error struct {
	Code int
	Msg  string
}

func (e *Error) Error() string { return e.Msg }
