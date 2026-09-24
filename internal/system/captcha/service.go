package captcha

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Cache 保存滑块坐标和二次校验。Redis 或内存都可以。
type Cache interface {
	Set(ctx context.Context, key, value string, ttl time.Duration) error
	GetDel(ctx context.Context, key string) (string, error)
}

// Challenge 是 /captcha/get 返回给滑块组件的数据。字段名对齐 AJ-Captcha。
type Challenge struct {
	Token               string `json:"token"`
	SecretKey           string `json:"secretKey"`
	OriginalImageBase64 string `json:"originalImageBase64"`
	JigsawImageBase64   string `json:"jigsawImageBase64"`
	CaptchaType         string `json:"captchaType"`
}

// Service 生成和校验滑块。误差允许 5 像素，和 AJ-Captcha 默认一致。
type Service struct {
	Cache Cache
	Now   func() time.Time
}

func (s *Service) Get(ctx context.Context) (*Challenge, error) {
	gap := randomGap()
	original, jigsaw, err := puzzle(gap)
	if err != nil {
		return nil, err
	}
	token := newToken()
	secret := randomKey()
	if err := s.Cache.Set(ctx, "captcha:"+token, fmt.Sprintf("%d|%s", gap, secret), 2*time.Minute); err != nil {
		return nil, err
	}
	return &Challenge{Token: token, SecretKey: secret, OriginalImageBase64: original, JigsawImageBase64: jigsaw, CaptchaType: "blockPuzzle"}, nil
}

// Check 校验前端传来的加密坐标，成功后给出登录要用的 captchaVerification。
func (s *Service) Check(ctx context.Context, token, pointJSON string) (string, error) {
	raw, err := s.Cache.GetDel(ctx, "captcha:"+token)
	if err != nil || raw == "" {
		return "", fmt.Errorf("验证码已失效")
	}
	gapText, secret, ok := strings.Cut(raw, "|")
	if !ok {
		return "", fmt.Errorf("验证码已失效")
	}
	plain, err := aesDecrypt(pointJSON, secret)
	if err != nil {
		return "", fmt.Errorf("验证码坐标不正确")
	}
	var point struct {
		X int `json:"x"`
	}
	if err := json.Unmarshal([]byte(plain), &point); err != nil {
		return "", fmt.Errorf("验证码坐标不正确")
	}
	gap, _ := strconv.Atoi(gapText)
	if point.X < gap-5 || point.X > gap+5 {
		return "", fmt.Errorf("验证码不正确")
	}
	verification, err := aesEncrypt(token+"---"+pointJSON, secret)
	if err != nil {
		return "", err
	}
	if err := s.Cache.Set(ctx, "captcha:ok:"+verification, "1", 2*time.Minute); err != nil {
		return "", err
	}
	return verification, nil
}

// Verify 给登录使用。二次校验只能用一次。
func newToken() string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

func (s *Service) Verify(ctx context.Context, verification string) error {
	if verification == "" {
		return fmt.Errorf("验证码不能为空")
	}
	value, err := s.Cache.GetDel(ctx, "captcha:ok:"+verification)
	if err != nil || value == "" {
		return fmt.Errorf("验证码不正确")
	}
	return nil
}
