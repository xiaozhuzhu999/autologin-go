package engine

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"sync"

	"github.com/getcharzp/go-ocr/ddddocr"
)

// GoOCRProvider 使用 Go 原生 ONNX Runtime 进行 OCR 识别
// 完全不依赖 Python，速度最快
type GoOCRProvider struct {
	engine   *ddddocr.Engine
	once     sync.Once
	initErr  error
}

var (
	goOCRInstance *GoOCRProvider
	goOCROnce     sync.Once
)

// GetGoOCRProvider 获取全局 Go OCR 单例
func GetGoOCRProvider(exeDir string) (*GoOCRProvider, error) {
	goOCROnce.Do(func() {
		goOCRInstance = &GoOCRProvider{}
		goOCRInstance.initErr = goOCRInstance.init(exeDir)
	})
	if goOCRInstance.initErr != nil {
		return nil, goOCRInstance.initErr
	}
	return goOCRInstance, nil
}

// init 初始化 ONNX Runtime 和模型
func (p *GoOCRProvider) init(exeDir string) error {
	libPath := filepath.Join(exeDir, "lib", "onnxruntime.dll")
	modelPath := filepath.Join(exeDir, "ddddocr_weights", "common_old.onnx")
	dictPath := filepath.Join(exeDir, "ddddocr_weights", "dict.txt")

	// 检查文件是否存在
	if _, err := os.Stat(libPath); err != nil {
		return fmt.Errorf("ONNX Runtime DLL 不存在: %s", libPath)
	}
	if _, err := os.Stat(modelPath); err != nil {
		return fmt.Errorf("OCR 模型不存在: %s", modelPath)
	}
	if _, err := os.Stat(dictPath); err != nil {
		return fmt.Errorf("字符集文件不存在: %s", dictPath)
	}

	config := ddddocr.Config{
		OnnxRuntimeLibPath: libPath,
		ModelPath:          modelPath,
		DictPath:           dictPath,
	}

	engine, err := ddddocr.NewEngine(config)
	if err != nil {
		return fmt.Errorf("Go OCR 引擎初始化失败: %w", err)
	}
	p.engine = engine
	return nil
}

// Recognize 识别验证码图片
func (p *GoOCRProvider) Recognize(imgBytes []byte) (string, error) {
	if p.engine == nil {
		return "", fmt.Errorf("Go OCR 引擎未初始化")
	}

	// 解码图片
	img, _, err := image.Decode(bytes.NewReader(imgBytes))
	if err != nil {
		return "", fmt.Errorf("图片解码失败: %w", err)
	}

	// 识别
	result, err := p.engine.Classification(img)
	if err != nil {
		return "", fmt.Errorf("OCR 识别失败: %w", err)
	}

	return result, nil
}

// Close 关闭引擎
func (p *GoOCRProvider) Close() {
	if p.engine != nil {
		p.engine.Destroy()
	}
}

// CloseGoOCR 关闭全局 Go OCR 引擎
func CloseGoOCR() {
	if goOCRInstance != nil {
		goOCRInstance.Close()
	}
}
