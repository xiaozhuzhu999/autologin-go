# AutoLoginPro

自动登录工具，支持验证码识别、动态口令、VPN 连接，提供 Web UI 管理站点配置。

## 技术栈

- **语言**: Go (无 Python 依赖)
- **GUI**: Chrome/Edge --app 模式 + HTML/CSS/JS
- **浏览器自动化**: chromedp
- **OCR**: Go 原生 ONNX Runtime (go-ocr + ddddocr 模型)
- **数据存储**: SQLite

## 项目结构

```
autologin-go/
  cmd/autologin/
    main.go              # 入口：HTTP 服务 + 浏览器启动
    web/                 # 前端页面 (首页/登录进度/站点编辑)
  internal/
    api/server.go        # REST API + SSE 实时推送
    db/db.go             # SQLite 数据层
    engine/
      engine.go          # 登录引擎核心流程
      ocr_go.go          # Go 原生 ONNX Runtime OCR
      totp.go            # TOTP 动态口令
      vpn.go             # VPN 连接
    models/site.go       # 数据模型
  ddddocr_weights/       # OCR 模型文件 (需自行获取)
  lib/                   # ONNX Runtime DLL (需自行获取)
  build.bat              # 编译脚本
```

## 编译

```bat
build.bat
```

需要 Go 1.22+，编译产出 `AutoLoginPro.exe`。

## 运行时依赖

以下大文件不包含在仓库中，需自行获取：

### 1. ONNX Runtime DLL

从 Python onnxruntime 包复制，或从 [ONNX Runtime Releases](https://github.com/microsoft/onnxruntime/releases) 下载：

```
lib/onnxruntime.dll              # ~15MB
lib/onnxruntime_providers_shared.dll  # ~22KB (已包含)
```

### 2. OCR 模型文件

从 ddddocr Python 包复制 `common_old.onnx`：

```
ddddocr_weights/common_old.onnx  # ~13MB
ddddocr_weights/dict.txt         # 字符集 (已包含)
```

获取方式：
```bash
pip install ddddocr
# 模型路径: Python安装目录/Lib/site-packages/ddddocr/common_old.onnx
```

### 3. 浏览器

需要系统已安装 Chrome 或 Edge 浏览器。

## 使用

1. 运行 `AutoLoginPro.exe`，自动打开浏览器窗口
2. 在首页添加站点（填写 URL、用户名、密码、选择器等）
3. 点击站点卡片上的「一键登录」按钮
4. 登录进度实时显示，验证码自动识别填写
5. 登录成功后浏览器保持打开，可继续操作
