package captcha

import (
	"crypto/aes"
	"encoding/base64"
	"errors"
)

// aesEncrypt 使用 AES/ECB/PKCS7，和 AJ-Captcha 的前端加密方式一致。
func aesEncrypt(plain, key string) (string, error) {
	block, err := aes.NewCipher(fitKey(key))
	if err != nil {
		return "", err
	}
	src := pkcs7([]byte(plain), block.BlockSize())
	out := make([]byte, len(src))
	for i := 0; i < len(src); i += block.BlockSize() {
		block.Encrypt(out[i:i+block.BlockSize()], src[i:i+block.BlockSize()])
	}
	return base64.StdEncoding.EncodeToString(out), nil
}

func aesDecrypt(encoded, key string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(fitKey(key))
	if err != nil {
		return "", err
	}
	if len(raw) == 0 || len(raw)%block.BlockSize() != 0 {
		return "", errors.New("密文长度不正确")
	}
	out := make([]byte, len(raw))
	for i := 0; i < len(raw); i += block.BlockSize() {
		block.Decrypt(out[i:i+block.BlockSize()], raw[i:i+block.BlockSize()])
	}
	out, err = pkcs7Unpad(out, block.BlockSize())
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func fitKey(key string) []byte {
	buf := make([]byte, 16)
	copy(buf, key)
	return buf
}

func pkcs7(src []byte, size int) []byte {
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
		return nil, errors.New("填充不正确")
	}
	pad := int(src[len(src)-1])
	if pad == 0 || pad > size || pad > len(src) {
		return nil, errors.New("填充不正确")
	}
	return src[:len(src)-pad], nil
}
