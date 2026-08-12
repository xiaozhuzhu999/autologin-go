package engine

import (
	"bytes"
	"fmt"
	"image"
	_ "image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestGoOCR_Recognition(t *testing.T) {
	// 定位项目根目录
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("无法获取工作目录: %v", err)
	}
	// internal/engine -> 上两级到项目根目录
	projectRoot := filepath.Join(wd, "..", "..")
	projectRoot, _ = filepath.Abs(projectRoot)

	// 初始化 Go OCR 引擎
	provider, err := GetGoOCRProvider(projectRoot)
	if err != nil {
		t.Fatalf("Go OCR 引擎初始化失败: %v", err)
	}
	defer provider.Close()

	t.Logf("Go OCR 引擎初始化成功")

	// 读取测试验证码图片
	testImgPath := filepath.Join(os.Getenv("TEMP"), "test_captcha.png")
	imgBytes, err := os.ReadFile(testImgPath)
	if err != nil {
		// 尝试其他路径
		testImgPath = filepath.Join("c:\\Users\\18343\\.trae-cn\\work\\6a7931b7a54ccb41c4bb4ed7", "test_captcha.png")
		imgBytes, err = os.ReadFile(testImgPath)
		if err != nil {
			t.Skipf("测试图片不存在，跳过: %v", err)
		}
	}

	t.Logf("测试图片: %s (%d 字节)", testImgPath, len(imgBytes))

	// 验证图片可以解码
	img, _, err := image.Decode(bytes.NewReader(imgBytes))
	if err != nil {
		t.Fatalf("图片解码失败: %v", err)
	}
	t.Logf("图片尺寸: %dx%d", img.Bounds().Dx(), img.Bounds().Dy())

	// 执行 OCR 识别
	result, err := provider.Recognize(imgBytes)
	if err != nil {
		t.Fatalf("OCR 识别失败: %v", err)
	}

	t.Logf("Go OCR 识别结果: %s", result)

	if result == "" {
		t.Error("OCR 返回空结果")
	}

	// 打印结果用于人工对比
	fmt.Printf("\n========== Go OCR 测试结果 ==========\n")
	fmt.Printf("识别结果: %s\n", result)
	fmt.Printf("Python ddddocr 结果: hrme\n")
	fmt.Printf("======================================\n")
}
