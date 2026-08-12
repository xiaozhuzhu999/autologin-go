# -*- coding: utf-8 -*-
"""
OCR 辅助脚本 - 供 Go 引擎调用
从 stdin 读取验证码图片字节数据，输出识别结果到 stdout
"""

import sys

def main():
    try:
        import ddddocr
        img_bytes = sys.stdin.buffer.read()
        if not img_bytes:
            print("", end="")
            return
        ocr = ddddocr.DdddOcr(show_ad=False)
        result = ocr.classification(img_bytes)
        print(result.strip(), end="")
    except Exception:
        print("", end="")

if __name__ == "__main__":
    main()
