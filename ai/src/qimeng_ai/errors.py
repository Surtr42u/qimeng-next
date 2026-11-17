"""只向模型输出稳定错误和可行动的状态，不传播凭据或文件路径。"""
from __future__ import annotations


class ToolError(Exception):
    def __init__(self, code: str, message: str, *, status: int | None = None,
                 uncertain: bool = False, details: dict | None = None):
        super().__init__(message)
        self.code, self.message = code, message
        self.status, self.uncertain, self.details = status, uncertain, details or {}

    def as_dict(self) -> dict:
        result = {"status": "error", "code": self.code, "message": self.message,
                  "operationMayHaveCompleted": self.uncertain}
        if self.status is not None:
            result["httpStatus"] = self.status
        if self.details:
            result["details"] = self.details
        return result
