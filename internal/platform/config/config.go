// Package config 读取进程配置。
// 三种启动入口共用这一份结构：单体关掉 Nacos，模块启动打开注册。
package config

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v3"
)

// Config 是一个进程启动所需的全部配置。
type Config struct {
	App     App     `yaml:"app"`
	HTTP    HTTP    `yaml:"http"`
	MySQL   MySQL   `yaml:"mysql"`
	Redis   Redis   `yaml:"redis"`
	Nacos   Nacos   `yaml:"nacos"`
	Auth    Auth    `yaml:"auth"`
	MyBatis MyBatis `yaml:"mybatis"`
}

// Auth 是登录相关配置。BcryptCost 为 0 时使用 bcrypt 的默认成本 10。
// 短信验证码两端都为 0 时，运行时使用 100000–999999，不再把 Java 测试用的 9999 当作线上默认值。
// 只有显式配置 smsCodeBegin/smsCodeEnd 才会收窄范围。
type Auth struct {
	CaptchaEnable bool `yaml:"captchaEnable"`
	BcryptCost    int  `yaml:"bcryptCost"`
	SmsCodeBegin  int  `yaml:"smsCodeBegin"`
	SmsCodeEnd    int  `yaml:"smsCodeEnd"`
}

func (a Auth) EffectiveSmsCodeBegin() int {
	if a.SmsCodeBegin == 0 && a.SmsCodeEnd == 0 {
		return 100000
	}
	return a.SmsCodeBegin
}

func (a Auth) EffectiveSmsCodeEnd() int {
	if a.SmsCodeBegin == 0 && a.SmsCodeEnd == 0 {
		return 999999
	}
	return a.SmsCodeEnd
}

// MyBatis 保存与 Java EncryptTypeHandler 对齐的字段密钥。
// 必须由部署方显式提供；公开代码不能内置能解密生产数据的默认密钥。
type MyBatis struct {
	EncryptorPassword string `yaml:"encryptorPassword"`
}

func (m MyBatis) EffectiveEncryptorPassword() string {
	return m.EncryptorPassword
}

// App 标识这个进程在集群里的名字，要和 Java 的 spring.application.name 对齐。
type App struct {
	Name string `yaml:"name"`
}

const (
	defaultUploadMaxFileBytes    int64 = 16 << 20
	defaultUploadMaxRequestBytes int64 = 32 << 20
	defaultReadTimeoutSeconds    int64 = 120
)

// HTTP 是本进程对外监听与接收请求的配置。
// 上传上限与 Java 的 spring.servlet.multipart 16/32 MiB 对齐；读取超时覆盖请求头和请求体。
type HTTP struct {
	Addr                  string `yaml:"addr"`
	UploadMaxFileBytes    int64  `yaml:"uploadMaxFileBytes"`
	UploadMaxRequestBytes int64  `yaml:"uploadMaxRequestBytes"`
	ReadTimeoutSeconds    int64  `yaml:"readTimeoutSeconds"`
}

// Effective* 对直接构造 Config 的测试和嵌入场景也提供安全默认值。
func (h HTTP) EffectiveUploadMaxFileBytes() int64 {
	if h.UploadMaxFileBytes == 0 {
		return defaultUploadMaxFileBytes
	}
	return h.UploadMaxFileBytes
}

func (h HTTP) EffectiveUploadMaxRequestBytes() int64 {
	if h.UploadMaxRequestBytes == 0 {
		return defaultUploadMaxRequestBytes
	}
	return h.UploadMaxRequestBytes
}

func (h HTTP) EffectiveReadTimeoutSeconds() int64 {
	if h.ReadTimeoutSeconds == 0 {
		return defaultReadTimeoutSeconds
	}
	return h.ReadTimeoutSeconds
}

// MySQL 使用 database/sql 的 DSN。连不上就拒绝启动，避免 /health 把空进程报成健康。
type MySQL struct {
	DSN string `yaml:"dsn"`
}

// Redis 保存令牌、字典和验证码。Redisson 在 Java 里只是客户端，这里直接连 Redis。
type Redis struct {
	Addr     string `yaml:"addr"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
}

// Nacos 只在模块启动时打开。单体 yudao-server 对应 Enabled=false。
// GrpcPort 为 0 时按 Nacos 默认用 HTTP 端口加 1000。
// 测试容器会把 8848 和 9848 映射成两个互不相干的宿主机端口，这时必须显式填写。
type Nacos struct {
	Enabled      bool   `yaml:"enabled"`
	Addr         string `yaml:"addr"`
	GrpcPort     uint64 `yaml:"grpcPort"`
	Namespace    string `yaml:"namespace"`
	Group        string `yaml:"group"`
	ServiceName  string `yaml:"serviceName"`
	RegisterIP   string `yaml:"registerIP"`
	RegisterPort uint64 `yaml:"registerPort"`
	Username     string `yaml:"username"`
	Password     string `yaml:"password"`
	Version      string `yaml:"version"`
	Tag          string `yaml:"tag"`
}

// Monolith 对应 Java 的 yudao-server：一个进程，不注册到 Nacos。
func Monolith() Config {
	cfg := base()
	cfg.App.Name = "yudao-server"
	cfg.HTTP.Addr = ":48080"
	cfg.Nacos.Enabled = false
	return cfg
}

// System 对应 Java 的 system-server。
func System() Config {
	cfg := base()
	cfg.App.Name = "system-server"
	cfg.HTTP.Addr = ":48081"
	cfg.Nacos.Enabled = true
	cfg.Nacos.ServiceName = "system-server"
	cfg.Nacos.RegisterPort = 48081
	return cfg
}

// Infra 对应 Java 的 infra-server。
func Infra() Config {
	cfg := base()
	cfg.App.Name = "infra-server"
	cfg.HTTP.Addr = ":48082"
	cfg.Nacos.Enabled = true
	cfg.Nacos.ServiceName = "infra-server"
	cfg.Nacos.RegisterPort = 48082
	return cfg
}

func base() Config {
	return Config{
		HTTP: HTTP{
			UploadMaxFileBytes:    defaultUploadMaxFileBytes,
			UploadMaxRequestBytes: defaultUploadMaxRequestBytes,
			ReadTimeoutSeconds:    defaultReadTimeoutSeconds,
		},
		Redis: Redis{Addr: "127.0.0.1:6379"},
		Nacos: Nacos{
			Addr:       "127.0.0.1:8848",
			Namespace:  "dev",
			Group:      "DEFAULT_GROUP",
			RegisterIP: "127.0.0.1",
			Version:    "1.0.0",
		},
	}
}

// Load 先用 base 作为缺省，再用 YAML 覆盖，最后用环境变量覆盖。
// 环境变量留给测试和部署，避免为了改一个端口去改文件。
func Load(path string, base Config) (Config, error) {
	if path != "" {
		body, err := os.ReadFile(path)
		if err != nil {
			return Config{}, fmt.Errorf("读取配置 %s: %w", path, err)
		}
		if err := yaml.Unmarshal(body, &base); err != nil {
			return Config{}, fmt.Errorf("解析配置 %s: %w", path, err)
		}
	}
	if err := applyEnv(&base); err != nil {
		return Config{}, err
	}
	if err := base.Validate(); err != nil {
		return Config{}, err
	}
	return base, nil
}

// Validate 在连数据库之前把明显的配置错误挡下。
func (c Config) Validate() error {
	if c.App.Name == "" {
		return fmt.Errorf("app.name 不能为空")
	}
	if c.HTTP.Addr == "" {
		return fmt.Errorf("http.addr 不能为空")
	}
	if c.HTTP.EffectiveUploadMaxFileBytes() <= 0 {
		return fmt.Errorf("http.uploadMaxFileBytes 必须大于 0")
	}
	if c.HTTP.EffectiveUploadMaxFileBytes() == math.MaxInt64 {
		return fmt.Errorf("http.uploadMaxFileBytes 不能达到 int64 上限")
	}
	if c.HTTP.EffectiveUploadMaxRequestBytes() < c.HTTP.EffectiveUploadMaxFileBytes() {
		return fmt.Errorf("http.uploadMaxRequestBytes 不能小于 http.uploadMaxFileBytes")
	}
	if c.HTTP.EffectiveReadTimeoutSeconds() <= 0 {
		return fmt.Errorf("http.readTimeoutSeconds 必须大于 0")
	}
	if c.HTTP.EffectiveReadTimeoutSeconds() > math.MaxInt64/int64(time.Second) {
		return fmt.Errorf("http.readTimeoutSeconds 超出可表示范围")
	}
	if c.MySQL.DSN == "" {
		return fmt.Errorf("mysql.dsn 不能为空")
	}
	if c.Redis.Addr == "" {
		return fmt.Errorf("redis.addr 不能为空")
	}
	if c.Redis.DB < 0 {
		return fmt.Errorf("redis.db 不能小于 0")
	}
	if c.Auth.BcryptCost != 0 && (c.Auth.BcryptCost < bcrypt.MinCost || c.Auth.BcryptCost > 31) {
		return fmt.Errorf("auth.bcryptCost 必须为 0 或 %d 到 31", bcrypt.MinCost)
	}
	if (c.Auth.SmsCodeBegin == 0) != (c.Auth.SmsCodeEnd == 0) {
		return fmt.Errorf("auth.smsCodeBegin 和 auth.smsCodeEnd 必须同时配置")
	}
	if c.Auth.SmsCodeEnd < c.Auth.SmsCodeBegin || c.Auth.SmsCodeBegin < 0 {
		return fmt.Errorf("auth.smsCodeEnd 不能小于 auth.smsCodeBegin")
	}
	if c.MyBatis.EffectiveEncryptorPassword() == "" {
		return fmt.Errorf("mybatis.encryptorPassword 不能为空，请通过环境变量或配置文件提供")
	}
	switch len(c.MyBatis.EffectiveEncryptorPassword()) {
	case 16, 24, 32:
	default:
		return fmt.Errorf("mybatis.encryptorPassword 长度必须是 16、24 或 32 字节")
	}
	if !c.Nacos.Enabled {
		return nil
	}
	if c.Nacos.Addr == "" {
		return fmt.Errorf("打开 Nacos 时 nacos.addr 不能为空")
	}
	if c.Nacos.ServiceName == "" {
		return fmt.Errorf("打开 Nacos 时 nacos.serviceName 不能为空")
	}
	if c.Nacos.Group == "" {
		return fmt.Errorf("打开 Nacos 时 nacos.group 不能为空")
	}
	if c.Nacos.Version == "" {
		return fmt.Errorf("打开 Nacos 时 nacos.version 不能为空")
	}
	if c.Nacos.GrpcPort > 65535 || c.Nacos.RegisterPort > 65535 {
		return fmt.Errorf("Nacos 端口不能超过 65535")
	}
	return nil
}

func applyEnv(c *Config) error {
	setString("YUDAO_APP_NAME", &c.App.Name)
	setString("YUDAO_HTTP_ADDR", &c.HTTP.Addr)
	if err := setInt64("YUDAO_HTTP_UPLOAD_MAX_FILE_BYTES", &c.HTTP.UploadMaxFileBytes); err != nil {
		return err
	}
	if err := setInt64("YUDAO_HTTP_UPLOAD_MAX_REQUEST_BYTES", &c.HTTP.UploadMaxRequestBytes); err != nil {
		return err
	}
	if err := setInt64("YUDAO_HTTP_READ_TIMEOUT_SECONDS", &c.HTTP.ReadTimeoutSeconds); err != nil {
		return err
	}
	setString("YUDAO_MYSQL_DSN", &c.MySQL.DSN)
	setString("YUDAO_REDIS_ADDR", &c.Redis.Addr)
	setString("YUDAO_REDIS_PASSWORD", &c.Redis.Password)
	if v, ok := os.LookupEnv("YUDAO_REDIS_DB"); ok && v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("YUDAO_REDIS_DB 必须是整数: %w", err)
		}
		c.Redis.DB = n
	}
	if v, ok := os.LookupEnv("YUDAO_NACOS_ENABLED"); ok && v != "" {
		enabled, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("YUDAO_NACOS_ENABLED 必须是布尔值: %w", err)
		}
		c.Nacos.Enabled = enabled
	}
	setString("YUDAO_NACOS_ADDR", &c.Nacos.Addr)
	if err := setUint("YUDAO_NACOS_GRPC_PORT", &c.Nacos.GrpcPort); err != nil {
		return err
	}
	setString("YUDAO_NACOS_NAMESPACE", &c.Nacos.Namespace)
	setString("YUDAO_NACOS_GROUP", &c.Nacos.Group)
	setString("YUDAO_NACOS_SERVICE_NAME", &c.Nacos.ServiceName)
	setString("YUDAO_NACOS_REGISTER_IP", &c.Nacos.RegisterIP)
	if err := setUint("YUDAO_NACOS_REGISTER_PORT", &c.Nacos.RegisterPort); err != nil {
		return err
	}
	setString("YUDAO_NACOS_USERNAME", &c.Nacos.Username)
	setString("YUDAO_NACOS_PASSWORD", &c.Nacos.Password)
	setString("YUDAO_NACOS_VERSION", &c.Nacos.Version)
	setString("YUDAO_NACOS_TAG", &c.Nacos.Tag)
	if v, ok := os.LookupEnv("YUDAO_CAPTCHA_ENABLE"); ok && v != "" {
		enabled, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("YUDAO_CAPTCHA_ENABLE 必须是布尔值: %w", err)
		}
		c.Auth.CaptchaEnable = enabled
	}
	if v, ok := os.LookupEnv("YUDAO_BCRYPT_COST"); ok && v != "" {
		cost, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("YUDAO_BCRYPT_COST 必须是整数: %w", err)
		}
		c.Auth.BcryptCost = cost
	}
	if v, ok := os.LookupEnv("YUDAO_SMS_CODE_BEGIN"); ok && v != "" {
		begin, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("YUDAO_SMS_CODE_BEGIN 必须是整数: %w", err)
		}
		c.Auth.SmsCodeBegin = begin
	}
	if v, ok := os.LookupEnv("YUDAO_SMS_CODE_END"); ok && v != "" {
		end, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("YUDAO_SMS_CODE_END 必须是整数: %w", err)
		}
		c.Auth.SmsCodeEnd = end
	}
	if v, ok := os.LookupEnv("YUDAO_MYBATIS_ENCRYPTOR_PASSWORD"); ok && v != "" {
		c.MyBatis.EncryptorPassword = v
	}
	return nil
}

func setInt64(key string, dst *int64) error {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return fmt.Errorf("%s 必须是整数: %w", key, err)
	}
	*dst = n
	return nil
}

func setString(key string, dst *string) {
	// 显式空值也要覆盖 YAML/缺省值；否则空的生产密钥可能意外退回开发配置。
	if v, ok := os.LookupEnv(key); ok {
		*dst = v
	}
}

func setUint(key string, dst *uint64) error {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return nil
	}
	n, err := strconv.ParseUint(v, 10, 64)
	if err != nil {
		return fmt.Errorf("%s 必须是非负整数: %w", key, err)
	}
	*dst = n
	return nil
}
