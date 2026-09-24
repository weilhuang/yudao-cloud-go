package datasource

import (
	"crypto/aes"
	"encoding/base64"
	"errors"
)

// Hutool SecureUtil.aes 使用 AES/ECB/PKCS5Padding，密文再做标准 Base64。

// EncryptBase64 把明文收成 Java EncryptTypeHandler.encrypt 的 Base64 密文。
func EncryptBase64(key, plain string) (string, error) {
	block, err := aes.NewCipher([]byte(key))
	if err != nil {
		return "", err
	}
	src := pkcs7Pad([]byte(plain), block.BlockSize())
	dst := make([]byte, len(src))
	for i := 0; i < len(src); i += block.BlockSize() {
		block.Encrypt(dst[i:i+block.BlockSize()], src[i:i+block.BlockSize()])
	}
	return base64.StdEncoding.EncodeToString(dst), nil
}

// DecryptBase64 还原 EncryptTypeHandler 写入的密码。
func DecryptBase64(key, encoded string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher([]byte(key))
	if err != nil {
		return "", err
	}
	if len(raw) == 0 || len(raw)%block.BlockSize() != 0 {
		return "", errors.New("密文长度不正确")
	}
	dst := make([]byte, len(raw))
	for i := 0; i < len(raw); i += block.BlockSize() {
		block.Decrypt(dst[i:i+block.BlockSize()], raw[i:i+block.BlockSize()])
	}
	plain, err := pkcs7Unpad(dst, block.BlockSize())
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func pkcs7Pad(src []byte, size int) []byte {
	pad := size - len(src)%size
	out := make([]byte, len(src)+pad)
	copy(out, src)
	for i := len(src); i < len(out); i++ {
		out[i] = byte(pad)
	}
	return out
}

func pkcs7Unpad(src []byte, size int) ([]byte, error) {
	if len(src) == 0 || len(src)%size != 0 {
		return nil, errors.New("填充长度不正确")
	}
	pad := int(src[len(src)-1])
	if pad == 0 || pad > size || pad > len(src) {
		return nil, errors.New("填充内容不正确")
	}
	for _, b := range src[len(src)-pad:] {
		if int(b) != pad {
			return nil, errors.New("填充内容不正确")
		}
	}
	return src[:len(src)-pad], nil
}
