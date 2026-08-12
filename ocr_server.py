# -*- coding: utf-8 -*-
"""
OCR 常驻服务 - 供 Go 引擎调用
启动后常驻内存，通过 stdin/stdout 协议通信，避免反复加载 ddddocr 模型

协议：
  输入: 先读 4 字节大端 uint32 表示图片长度，再读对应字节数的图片数据
  输出: 先写 4 字节大端 uint32 表示结果长度，再写对应字节数的结果文本
  输入长度为 0 表示退出
"""

import sys
import struct

def main():
    try:
        import ddddocr
        ocr = ddddocr.DdddOcr(show_ad=False)
    except Exception as e:
        # 写入错误信息
        err = f"INIT_ERROR: {e}".encode("utf-8")
        sys.stdout.buffer.write(struct.pack(">I", len(err)))
        sys.stdout.buffer.write(err)
        sys.stdout.buffer.flush()
        return

    # 写入就绪信号
    ready = b"READY"
    sys.stdout.buffer.write(struct.pack(">I", len(ready)))
    sys.stdout.buffer.write(ready)
    sys.stdout.buffer.flush()

    while True:
        try:
            # 读取图片长度
            header = sys.stdin.buffer.read(4)
            if len(header) < 4:
                break
            length = struct.unpack(">I", header)[0]
            if length == 0:
                break

            # 读取图片数据
            img_bytes = sys.stdin.buffer.read(length)
            if len(img_bytes) < length:
                break

            # 识别
            result = ocr.classification(img_bytes)
            result_bytes = result.strip().encode("utf-8")

            # 写入结果
            sys.stdout.buffer.write(struct.pack(">I", len(result_bytes)))
            sys.stdout.buffer.write(result_bytes)
            sys.stdout.buffer.flush()

        except Exception:
            # 写入空结果
            sys.stdout.buffer.write(struct.pack(">I", 0))
            sys.stdout.buffer.flush()

if __name__ == "__main__":
    main()
