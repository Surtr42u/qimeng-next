"""有界本地探测/抽帧；不猜作者，不写业务，不自动外传媒体。"""
from __future__ import annotations

import asyncio
import base64
import hashlib
import json
import math
import os
from pathlib import Path
import shutil
import subprocess
import tempfile

from .content import ImageResult
from .errors import ToolError
from .files import CHUNK_BYTES, identity, open_reference

MAX_SOURCE_BYTES = 2 * 1024 * 1024 * 1024
MAX_IMAGE_BYTES = 2 * 1024 * 1024
TEMP_RESERVE = 64 * 1024 * 1024
DEMUXERS = {".png": "png_pipe", ".jpg": "mjpeg", ".jpeg": "mjpeg", ".webp": "webp_pipe",
            ".gif": "gif", ".mp4": "mov", ".mov": "mov", ".m4v": "mov", ".mkv": "matroska", ".webm": "matroska"}


def tool_definitions():
    return [
        {"name": "research.probe", "description": "对已授权fileRef探测并抽取少量预览帧；只产候选研究材料，零业务写入。不要求作者名。",
         "scope": "local.research", "readOnly": True,
         "inputSchema": {"type": "object", "properties": {"fileRef": {"type": "string"},
                         "frames": {"type": "integer", "minimum": 0, "maximum": 3, "default": 3}},
                         "required": ["fileRef"], "additionalProperties": False}},
        {"name": "platform.artifact.read", "description": "读取预览frame的标准MCP image内容；必须有独立媒体外传许可，引用不扩大授权。",
         "scope": "read", "readOnly": True,
         "inputSchema": {"type": "object", "properties": {"artifactRef": {"type": "string"}},
                         "required": ["artifactRef"], "additionalProperties": False}},
    ]


async def _process(arguments: list[str], maximum: int, timeout: int) -> bytes:
    flags = subprocess.CREATE_NO_WINDOW if os.name == "nt" else 0
    process = await asyncio.create_subprocess_exec(*arguments, stdin=subprocess.DEVNULL,
        stdout=asyncio.subprocess.PIPE, stderr=subprocess.DEVNULL, creationflags=flags)
    try:
        async with asyncio.timeout(timeout):
            result = await process.stdout.read(maximum + 1)
            if len(result) > maximum:
                raise ToolError("RESEARCH_OUTPUT_LIMIT", "媒体工具输出超过资源预算")
            # read(n) can return early on a pipe; continue with the remaining bound.
            while True:
                chunk = await process.stdout.read(maximum + 1 - len(result))
                if not chunk:
                    break
                result += chunk
                if len(result) > maximum:
                    raise ToolError("RESEARCH_OUTPUT_LIMIT", "媒体工具输出超过资源预算")
            if await process.wait() != 0:
                raise ToolError("MEDIA_PROBE_FAILED", "媒体工具未成功解码；保留原件并报告未验证")
            return result
    except TimeoutError:
        raise ToolError("RESEARCH_TIMEOUT", "媒体探测超时，未修改原件或业务状态") from None
    finally:
        if process.returncode is None:
            process.kill()
            await asyncio.wait_for(process.communicate(), timeout=5)


def probe(runtime, arguments: dict) -> dict:
    ffprobe, ffmpeg = shutil.which("ffprobe"), shutil.which("ffmpeg")
    if not ffprobe or not ffmpeg:
        raise ToolError("MEDIA_DEPENDENCY_MISSING", "执行环境需要ffprobe与ffmpeg；不能将工具缺失视为研究无结果")
    record = runtime.state.read("r_" + arguments["fileRef"][2:], kind="file")
    extension = Path(record["relative"]).suffix.lower()
    if extension not in DEMUXERS:
        raise ToolError("MEDIA_UNSUPPORTED", "该格式尚无受限探测通道")
    with open_reference(runtime, arguments["fileRef"]) as (source, info):
        if info.st_size > MAX_SOURCE_BYTES:
            raise ToolError("RESEARCH_SOURCE_LIMIT", "当前本地快照探测上限为2GiB；不影响正式上传能力")
        if shutil.disk_usage(runtime.config.state_dir).free < info.st_size + TEMP_RESERVE:
            raise ToolError("RESEARCH_SPACE", "执行目录不足以保留探测快照及余量")
        with tempfile.TemporaryDirectory(prefix="research-", dir=runtime.config.state_dir) as temporary:
            snapshot = Path(temporary) / ("input" + extension)
            digest = hashlib.sha256()
            with snapshot.open("xb") as output:
                remaining = info.st_size
                while remaining:
                    chunk = source.read(min(CHUNK_BYTES, remaining))
                    if not chunk:
                        raise ToolError("FILE_REF_STALE", "探测快照期间来源变短")
                    digest.update(chunk)
                    output.write(chunk)
                    remaining -= len(chunk)
                if source.read(1):
                    raise ToolError("FILE_REF_STALE", "探测快照期间来源变长")
            if identity(os.fstat(source.fileno())) != identity(info):
                raise ToolError("FILE_REF_STALE", "探测快照期间来源变化")
            source_hash = digest.hexdigest()
            common = ["-protocol_whitelist", "file,pipe", "-f", DEMUXERS[extension]]
            data = asyncio.run(_process([ffprobe, "-v", "error", *common,
                "-show_entries", "format=duration,format_name:stream=codec_type,codec_name,width,height",
                "-of", "json", str(snapshot)], 64 * 1024, min(runtime.config.timeout, 120)))
            try:
                metadata = json.loads(data)
                duration = float(metadata.get("format", {}).get("duration", 0))
            except (ValueError, TypeError):
                raise ToolError("MEDIA_METADATA_INVALID", "媒体探测未返回有效元数据") from None
            if not math.isfinite(duration) or duration < 0:
                duration = 0
            frames = []
            frame_count = arguments.get("frames", 3)
            positions = ([duration * ratio for ratio in (.15, .5, .85)][:frame_count]
                         if extension in {".mp4", ".m4v", ".mov", ".mkv", ".webm"} else [0][:frame_count])
            for position in positions:
                image = asyncio.run(_process([ffmpeg, "-v", "error", "-nostdin", "-threads", "2", *common,
                    "-ss", str(position), "-i", str(snapshot), "-vf", "scale=640:640:force_original_aspect_ratio=decrease",
                    "-frames:v", "1", "-threads", "1", "-f", "image2pipe", "-vcodec", "png", "pipe:1"],
                    MAX_IMAGE_BYTES, min(runtime.config.timeout, 120)))
                if not image.startswith(b"\x89PNG\r\n\x1a\n"):
                    raise ToolError("FRAME_INVALID", "抽帧未产生有效PNG内容")
                image_hash = hashlib.sha256(image).hexdigest()
                reference = runtime.state.save({"data": base64.b64encode(image).decode(), "sha256": image_hash,
                    "sourceSha256": source_hash, "mimeType": "image/png", "positionSeconds": position,
                    "storage": record["storage"],
                    "rootBinding": hashlib.sha256(str(runtime.config.storages[record["storage"]].resolve()).encode()).hexdigest()}, kind="image")
                frames.append({"artifactRef": "a_" + reference[2:], "sha256": image_hash,
                               "positionSeconds": position, "mimeType": "image/png"})
            return {"status": "ready", "businessWrites": 0, "sourceSha256": source_hash,
                    "metadata": metadata, "frameRefs": frames, "candidates": [],
                    "next": "仅使用获授权的外部研究工具核实候选；返回证据后提交research.submit。",
                    "limitations": ["探测/抽帧不证明作者归属", "尚未进行外部原帖核实"]}


def read_artifact(runtime, reference: str) -> ImageResult:
    if not runtime.config.allow_media_to_model:
        raise ToolError("MEDIA_TRANSFER_NOT_AUTHORIZED", "未配置向模型发送媒体的独立授权")
    if reference.startswith("sa_"):
        if "research" not in runtime.config.permissions:
            raise ToolError("PERMISSION_DENIED", "服务器研究材料需要research权限")
        record = runtime.transport.call("getResearchArtifact", {"id": reference[3:]})
        data, mime = runtime.transport.binary("getResearchArtifactContent", {"id": reference[3:]}, maximum=MAX_IMAGE_BYTES)
        if (record.get("artifactRef") != reference or mime != "image/jpeg" or record.get("mimeType") != mime or
                record.get("size") != len(data) or hashlib.sha256(data).hexdigest() != record.get("sha256")):
            raise ToolError("ARTIFACT_CORRUPT", "服务器研究材料的来源或内容摘要不一致")
        return ImageResult(record, data, mime)
    if "local.research" not in runtime.config.permissions:
        raise ToolError("PERMISSION_DENIED", "本地研究材料需要local.research权限")
    if not reference.startswith("a_"):
        raise ToolError("ARTIFACT_REF_INVALID", "不是研究图片引用")
    record = runtime.state.read("r_" + reference[2:], kind="image")
    root = runtime.config.storages.get(record["storage"])
    if ("local.read" not in runtime.config.permissions or root is None or
            hashlib.sha256(str(root.resolve()).encode()).hexdigest() != record["rootBinding"]):
        raise ToolError("ARTIFACT_PERMISSION_CHANGED", "原始存储授权已撤销或变化")
    try:
        data = base64.b64decode(record["data"], validate=True)
    except ValueError:
        raise ToolError("ARTIFACT_CORRUPT", "研究图片编码损坏") from None
    if len(data) > MAX_IMAGE_BYTES or hashlib.sha256(data).hexdigest() != record["sha256"]:
        raise ToolError("ARTIFACT_CORRUPT", "研究图片完整性校验失败")
    return ImageResult({key: value for key, value in record.items() if key not in {"data", "rootBinding", "storage"}}, data)
