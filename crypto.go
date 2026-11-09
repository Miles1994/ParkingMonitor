package main

import (
	"crypto/aes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"sync"
)

const publicKeyPEM = `-----BEGIN PUBLIC KEY-----
MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAido+JPjUrQOu7SWltkxAiIgI/Il5F9YFKqrehGl+yIFNnF6iTZ076q03sni4pdBw9Hm4THnt/irR9NsQlYwQ8jygVW6pp2Wouih8552o5b1/naXSmIFKmYnfi9jPe7VN4wzz/UgoCYFoNcS9/CRT9J0xwL+Y2BZMH9DjUYgMQs3xtpN6Xb2BVoPsnT8reBF3G/8Y5deWLzGCl13/ttAXrXtuJYXVDz2eLs9qeWsLkQptM2RE0JXwOrnmMk6ijAo1xYQF30wyDmxG6Zv6blbsEI4uKczLHCYyP2SR/X5PfvSlbJY58ILB7JTJez6YPTBGkrGluIWjA9b/fRNsAoQUtwIDAQAB
-----END PUBLIC KEY-----`

const aesKeyAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

var (
	pubKeyOnce sync.Once
	pubKey     *rsa.PublicKey
	pubKeyErr  error
)

func loadPublicKey() (*rsa.PublicKey, error) {
	pubKeyOnce.Do(func() {
		block, _ := pem.Decode([]byte(publicKeyPEM))
		if block == nil {
			pubKeyErr = errors.New("内置公钥解析失败")
			return
		}
		key, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			pubKeyErr = err
			return
		}
		pk, ok := key.(*rsa.PublicKey)
		if !ok {
			pubKeyErr = errors.New("内置公钥类型错误")
			return
		}
		pubKey = pk
	})
	return pubKey, pubKeyErr
}

func newAESKey() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	for i := range buf {
		buf[i] = aesKeyAlphabet[int(buf[i])%len(aesKeyAlphabet)]
	}
	return string(buf), nil
}

func pkcs7Pad(data []byte, blockSize int) []byte {
	pad := blockSize - len(data)%blockSize
	out := make([]byte, len(data)+pad)
	copy(out, data)
	for i := len(data); i < len(out); i++ {
		out[i] = byte(pad)
	}
	return out
}

func pkcs7Unpad(data []byte) ([]byte, error) {
	if len(data) == 0 || len(data)%aes.BlockSize != 0 {
		return nil, errors.New("密文长度无效")
	}
	pad := int(data[len(data)-1])
	if pad == 0 || pad > aes.BlockSize || pad > len(data) {
		return nil, errors.New("填充无效")
	}
	for _, b := range data[len(data)-pad:] {
		if int(b) != pad {
			return nil, errors.New("填充无效")
		}
	}
	return data[:len(data)-pad], nil
}

func aesECBEncrypt(key, plain []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	padded := pkcs7Pad(plain, block.BlockSize())
	out := make([]byte, len(padded))
	for i := 0; i < len(padded); i += block.BlockSize() {
		block.Encrypt(out[i:i+block.BlockSize()], padded[i:i+block.BlockSize()])
	}
	return out, nil
}

func aesECBDecrypt(key, cipherText []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(cipherText)%block.BlockSize() != 0 {
		return nil, errors.New("密文不是整块")
	}
	out := make([]byte, len(cipherText))
	for i := 0; i < len(cipherText); i += block.BlockSize() {
		block.Decrypt(out[i:i+block.BlockSize()], cipherText[i:i+block.BlockSize()])
	}
	return pkcs7Unpad(out)
}

// encryptRequestBody 复刻小程序 rsaCryptoAll：返回 JSON 字符串形式的 "RSA(aesKey)-AES-ECB(json)"。
func encryptRequestBody(payload []byte, aesKey string) (string, error) {
	pk, err := loadPublicKey()
	if err != nil {
		return "", err
	}
	rsaPart, err := rsa.EncryptPKCS1v15(rand.Reader, pk, []byte(aesKey))
	if err != nil {
		return "", fmt.Errorf("RSA 加密失败: %w", err)
	}
	cipherText, err := aesECBEncrypt([]byte(aesKey), payload)
	if err != nil {
		return "", fmt.Errorf("AES 加密失败: %w", err)
	}
	combined := base64.StdEncoding.EncodeToString(rsaPart) + "-" + base64.StdEncoding.EncodeToString(cipherText)
	quoted, err := json.Marshal(combined)
	if err != nil {
		return "", err
	}
	return string(quoted), nil
}

func decryptResponseData(encoded, aesKey string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("响应 base64 解码失败: %w", err)
	}
	return aesECBDecrypt([]byte(aesKey), raw)
}
