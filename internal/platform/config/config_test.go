package config

import (
	"os"
	"path/filepath"
	"testing"
)

const testEncryptorPassword = "0123456789abcdef"

func configuredMonolith() Config {
	cfg := Monolith()
	cfg.MySQL.DSN = "test-dsn"
	cfg.MyBatis.EncryptorPassword = testEncryptorPassword
	return cfg
}

func configuredSystem() Config {
	cfg := System()
	cfg.MySQL.DSN = "test-dsn"
	cfg.MyBatis.EncryptorPassword = testEncryptorPassword
	return cfg
}

func TestMonolithDoesNotRegister(t *testing.T) {
	cfg := Monolith()
	if cfg.Nacos.Enabled {
		t.Fatal("单体不应注册 Nacos")
	}
	if cfg.App.Name != "yudao-server" || cfg.HTTP.Addr != ":48080" {
		t.Fatalf("单体缺省不对: %+v", cfg.App)
	}
}

func TestModuleNamesMatchJava(t *testing.T) {
	system := System()
	infra := Infra()
	if system.Nacos.ServiceName != "system-server" || system.HTTP.Addr != ":48081" {
		t.Fatalf("system: %+v", system)
	}
	if infra.Nacos.ServiceName != "infra-server" || infra.HTTP.Addr != ":48082" {
		t.Fatalf("infra: %+v", infra)
	}
	if !system.Nacos.Enabled || !infra.Nacos.Enabled {
		t.Fatal("模块启动应打开 Nacos")
	}
}

func TestValidateRejectsEmptyDSN(t *testing.T) {
	cfg := configuredMonolith()
	cfg.MySQL.DSN = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("空 DSN 应失败")
	}
}

func TestValidateRejectsMissingEncryptorPassword(t *testing.T) {
	cfg := configuredMonolith()
	cfg.MyBatis.EncryptorPassword = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("公开版不能退回内置的数据库加密密钥")
	}
}

func TestValidateRejectsInvalidBcryptCost(t *testing.T) {
	cfg := configuredMonolith()
	cfg.Auth.BcryptCost = 32
	if err := cfg.Validate(); err == nil {
		t.Fatal("超出 bcrypt 支持范围的成本应在启动时失败")
	}
}

func TestSmsCodeRangeDefaultsAndValidation(t *testing.T) {
	cfg := configuredMonolith()
	if cfg.Auth.EffectiveSmsCodeBegin() != 100000 || cfg.Auth.EffectiveSmsCodeEnd() != 999999 {
		t.Fatalf("未配置时不能退回 9999：%d-%d", cfg.Auth.EffectiveSmsCodeBegin(), cfg.Auth.EffectiveSmsCodeEnd())
	}
	cfg.Auth.SmsCodeBegin = 9999
	if err := cfg.Validate(); err == nil {
		t.Fatal("只配置一端应失败")
	}
	cfg.Auth.SmsCodeEnd = 9999
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if cfg.Auth.EffectiveSmsCodeBegin() != 9999 || cfg.Auth.EffectiveSmsCodeEnd() != 9999 {
		t.Fatal("显式 9999 只用于测试环境")
	}
}

func TestValidateNacosRequiresServiceName(t *testing.T) {
	cfg := configuredSystem()
	cfg.Nacos.ServiceName = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("打开 Nacos 但没有服务名应失败")
	}
}

func TestLoadEnvOverridesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	body := []byte("http:\n  addr: \":9\"\nmysql:\n  dsn: from-file\n")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("YUDAO_MYSQL_DSN", "from-env")
	t.Setenv("YUDAO_MYBATIS_ENCRYPTOR_PASSWORD", testEncryptorPassword)
	t.Setenv("YUDAO_NACOS_ENABLED", "false")

	cfg, err := Load(path, Monolith())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MySQL.DSN != "from-env" {
		t.Fatalf("环境变量应覆盖文件, got %s", cfg.MySQL.DSN)
	}
	if cfg.HTTP.Addr != ":9" {
		t.Fatalf("文件应覆盖缺省端口, got %s", cfg.HTTP.Addr)
	}
	if cfg.Nacos.Enabled {
		t.Fatal("YUDAO_NACOS_ENABLED=false 应关闭注册")
	}
}

func TestHTTPUploadDefaultsMatchJava(t *testing.T) {
	cfg := Monolith()
	if cfg.HTTP.EffectiveUploadMaxFileBytes() != 16<<20 || cfg.HTTP.EffectiveUploadMaxRequestBytes() != 32<<20 {
		t.Fatalf("上传默认上限未对齐 Java: %+v", cfg.HTTP)
	}
	if cfg.HTTP.EffectiveReadTimeoutSeconds() != 120 {
		t.Fatalf("慢速请求读取超时应为 120 秒: %+v", cfg.HTTP)
	}
	// 直接构造配置的集成测试也必须有安全默认值。
	if (HTTP{}).EffectiveUploadMaxRequestBytes() != 32<<20 || (HTTP{}).EffectiveReadTimeoutSeconds() != 120 {
		t.Fatal("HTTP 零值不能关闭请求体上限或读取超时")
	}
}

func TestLoadHTTPUploadLimitsFromEnv(t *testing.T) {
	t.Setenv("YUDAO_HTTP_UPLOAD_MAX_FILE_BYTES", "1048576")
	t.Setenv("YUDAO_HTTP_UPLOAD_MAX_REQUEST_BYTES", "2097152")
	t.Setenv("YUDAO_HTTP_READ_TIMEOUT_SECONDS", "180")
	cfg, err := Load("", configuredMonolith())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTP.EffectiveUploadMaxFileBytes() != 1<<20 || cfg.HTTP.EffectiveUploadMaxRequestBytes() != 2<<20 || cfg.HTTP.EffectiveReadTimeoutSeconds() != 180 {
		t.Fatalf("环境变量未覆盖上传配置: %+v", cfg.HTTP)
	}
}

func TestLoadRejectsInvalidHTTPUploadLimits(t *testing.T) {
	for _, tc := range []struct {
		name, key, value string
	}{
		{"负数文件上限", "YUDAO_HTTP_UPLOAD_MAX_FILE_BYTES", "-1"},
		{"请求小于文件", "YUDAO_HTTP_UPLOAD_MAX_REQUEST_BYTES", "1"},
		{"负数读取超时", "YUDAO_HTTP_READ_TIMEOUT_SECONDS", "-1"},
		{"读取超时溢出", "YUDAO_HTTP_READ_TIMEOUT_SECONDS", "9223372037"},
		{"文件长度溢出", "YUDAO_HTTP_UPLOAD_MAX_FILE_BYTES", "9223372036854775807"},
		{"非法整数", "YUDAO_HTTP_UPLOAD_MAX_FILE_BYTES", "16MB"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)
			if _, err := Load("", configuredMonolith()); err == nil {
				t.Fatalf("%s=%s 应拒绝", tc.key, tc.value)
			}
		})
	}
}

func TestLoadRejectsInvalidOperationalEnv(t *testing.T) {
	for _, tc := range []struct {
		name, key, value string
	}{
		{"拼错 Nacos 开关", "YUDAO_NACOS_ENABLED", "fasle"},
		{"拼错验证码开关", "YUDAO_CAPTCHA_ENABLE", "falze"},
		{"非法 Redis 库号", "YUDAO_REDIS_DB", "default"},
		{"负数 Redis 库号", "YUDAO_REDIS_DB", "-1"},
		{"非法 Nacos gRPC 端口", "YUDAO_NACOS_GRPC_PORT", "9848x"},
		{"非法 Nacos 注册端口", "YUDAO_NACOS_REGISTER_PORT", "-1"},
		{"超出 Nacos 端口范围", "YUDAO_NACOS_REGISTER_PORT", "65536"},
		{"非法 bcrypt 成本", "YUDAO_BCRYPT_COST", "ten"},
		{"显式清空 MySQL DSN", "YUDAO_MYSQL_DSN", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)
			if _, err := Load("", configuredSystem()); err == nil {
				t.Fatalf("%s=%s 不应静默使用默认配置", tc.key, tc.value)
			}
		})
	}
}
