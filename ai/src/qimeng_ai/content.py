"""MCP媒体内容与JSON摘要分开预算，CLI默认只输出元信息。"""
from dataclasses import dataclass


@dataclass(frozen=True)
class ImageResult:
    metadata: dict
    data: bytes
    mime_type: str = "image/png"
