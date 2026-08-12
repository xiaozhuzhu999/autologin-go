package engine

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"time"
)

// GenerateTOTP 生成 TOTP 动态口令
// secret: 密钥（原始字符串）
// secretFormat: "base32" | "base64" | "plain"
// digits: 位数（通常 6）
// interval: 时间间隔（通常 30 秒）
func GenerateTOTP(secret, secretFormat string, digits, interval int) (string, error) {
	if digits <= 0 {
		digits = 6
	}
	if interval <= 0 {
		interval = 30
	}

	var key []byte
	var err error

	switch secretFormat {
	case "base32":
		key, err = base32.StdEncoding.DecodeString(secret)
		if err != nil {
			return "", fmt.Errorf("base32 解码失败: %w", err)
		}
	case "base64":
		key, err = base64.StdEncoding.DecodeString(secret)
		if err != nil {
			return "", fmt.Errorf("base64 解码失败: %w", err)
		}
		// Python 版将 base64 解码后转为 hex，Go 中直接使用原始字节
	default:
		key = []byte(secret)
	}

	counter := uint64(time.Now().Unix()) / uint64(interval)
	counterBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(counterBytes, counter)

	mac := hmac.New(sha1.New, key)
	mac.Write(counterBytes)
	hash := mac.Sum(nil)

	offset := int(hash[len(hash)-1] & 0x0F)
	truncated := binary.BigEndian.Uint32(hash[offset:offset+4]) & 0x7FFFFFFF

	otp := truncated % uint32(pow10(digits))
	return fmt.Sprintf("%0*d", digits, otp), nil
}

func pow10(n int) int {
	result := 1
	for i := 0; i < n; i++ {
		result *= 10
	}
	return result
}
