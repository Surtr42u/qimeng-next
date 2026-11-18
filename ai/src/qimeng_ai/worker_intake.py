"""Bind model intent to the worker's observed, sealed Go input and plan identities."""
from __future__ import annotations

from pathlib import PurePosixPath

from .errors import ToolError
from .state import encode


GROUPS = {
    "authors": {"author.query", "author.profile.summary"},
    "taxonomy": {"source.query", "character.query", "tag.vocabulary.query", "collection.query"},
    "naming": {"naming.context", "naming.suggest", "naming.preview"},
    "research": {"research.materials", "research.submit", "platform.artifact.read", "research.context",
                 "research.provider.query", "research.provider.result", "research.image.search"},
    "records": {"task.read", "platform.result.read", "platform.budget", "operation.read"},
}
ENTITY_FIELDS = {"authorId": ("authors", "author.query"), "authorIds": ("authors", "author.query"),
                 "sourceIds": ("taxonomy", "source.query"), "characterIds": ("taxonomy", "character.query"),
                 "collectionId": ("taxonomy", "collection.query")}
PAGE = 10


def obj(properties, required=()):
    return {"type": "object", "properties": properties, "required": list(required), "additionalProperties": False}


def options(names):
    return {"type": "string", "enum": sorted(names)}


def full_result(runtime, value):
    if isinstance(value, dict) and value.get("truncated") and value.get("resultRef"):
        return runtime.state.read(value["resultRef"])
    return value


class WorkerIntake:
    def __init__(self, runtime, batch_id, report):
        self.runtime, self.batch_id = runtime, batch_id
        self.plans = {}
        self.views = {}
        self.stale = set()
        self.details = set()
        self.entities = {query: set() for _, query in ENTITY_FIELDS.values()}
        self.entity_refs = {}
        self.shown_inputs = set()
        self.page_inputs = []
        self.excluded = []
        self.plan_exclusions = {}
        self.groups = set()
        self.update(full_result(runtime, report))

    def update(self, report):
        if report["batch"]["id"] != self.batch_id or report["batch"]["manifestState"] != "sealed":
            raise ToolError("TASK_RESPONSE_INVALID", "读取的清单不属于当前已封存任务")
        self.report = report
        self.by_number = {row["ordinal"] + 1: row for row in report["inputs"]}
        self.uploads = {row["upload"]["id"]: row for row in report["items"]}
        self.by_upload = {row["uploadId"]: number for number, row in self.by_number.items() if row.get("uploadId")}
        for plan in report["plans"]:
            self.remember(plan)

    def refresh(self):
        self.update(self.runtime.transport.call("getBatch", {"id": self.batch_id}))

    def remember(self, plan):
        for key, previous in self.plans.items():
            if previous["id"] == plan["id"]:
                if previous["fingerprint"] != plan["fingerprint"]:
                    raise ToolError("PLAN_REFERENCE_CHANGED", "原计划指纹已变化，停止使用本次预览")
                self.plans[key] = {**previous, **plan}
                return key
        key = "P" + str(len(self.plans) + 1)
        self.plans[key] = plan
        return key

    def page(self, rows, offset):
        if type(offset) is not int or not 0 <= offset <= len(rows):
            raise ToolError("INVALID_INPUT", "请使用上页返回的 offset")
        end = min(offset + PAGE, len(rows))
        chunk = []
        for row in rows[offset:end]:
            if chunk and len(encode(chunk + [row]).encode()) > self.runtime.config.output_bytes // 2:
                break
            chunk.append(row)
        return chunk, offset + len(chunk) if offset + len(chunk) < len(rows) else None

    def summary(self, offset=0):
        batch = self.report["batch"]
        rows = []
        for number, row in self.by_number.items():
            item = self.uploads.get(row.get("uploadId"), {})
            upload = item.get("upload", {})
            entry = {"input": number, "name": row.get("originalFileName") or upload.get("fileName"),
                     "state": row["state"]}
            if row["state"] == "staged":
                entry["canPrepare"] = item.get("researchOutcome") != "conflict" and not upload.get("replaceAssetId")
                # These are the sender's actual selections. Omitting edit fields
                # in the intent lets Go preserve them.
                for key in ("mimeType", "family", "authorIds", "characterIds", "sourceIds", "collectionId", "relativeDirectory", "note"):
                    if upload.get(key):
                        entry[key] = upload[key]
                if len(encode(entry).encode()) > self.runtime.config.output_bytes // 2:
                    reference = self.runtime.state.save(entry)
                    self.details.add(reference)
                    entry = {"input": number, "name": (entry["name"] or "")[:160], "state": row["state"],
                             "canPrepare": entry["canPrepare"], "resultRef": reference}
                if item.get("researchOutcome"):
                    entry["researchOutcome"] = item["researchOutcome"]
            if row.get("errorCode"):
                entry["reason"] = row["errorCode"]
            rows.append(entry)
        visible, next_offset = self.page(rows, offset)
        self.page_inputs = [row["input"] for row in visible]
        self.shown_inputs.update(row["input"] for row in visible)
        result = {"request": batch["label"], "state": batch["state"], "inputs": visible,
                  "readyInputs": [row["input"] for row in visible if row.get("canPrepare")],
                  "totalInputs": len(rows), "plans": [{"plan": key, "state": plan["state"], "requiresNewPlan": key in self.stale} for key, plan in self.plans.items()],
                  "remainingResearchCalls": batch["maxResearchCalls"] - batch["researchCalls"]}
        if next_offset is not None:
            result["nextOffset"] = next_offset
        return result

    def preview(self, plan, offset=0, excluded=None):
        if plan["batchId"] != self.batch_id:
            raise ToolError("PLAN_SCOPE_DENIED", "预览不属于当前任务")
        key = self.remember(plan)
        if excluded:
            self.plan_exclusions[key] = excluded
        rows = []
        for item in plan["items"]:
            number = self.by_upload.get(item["uploadId"])
            if number is None:
                raise ToolError("PLAN_SCOPE_DENIED", "计划包含当前封存清单以外的输入")
            row = {"input": number, "target": item["relativePath"], "authorIds": item["authorIds"]}
            for field in ("collectionId", "sourceIds", "characterIds", "page"):
                if item.get(field):
                    row[field] = item[field]
            rows.append(row)
        visible, next_offset = self.page(rows, offset)
        viewed = self.views.setdefault(key, set())
        viewed.update(range(offset, offset + len(visible)))
        result = {"plan": key, "state": plan["state"], "items": visible, "totalItems": len(rows), "previewComplete": len(viewed) == len(rows)}
        if key in self.plan_exclusions:
            result["notPlanned"] = self.plan_exclusions[key]
        if next_offset is not None:
            result["nextOffset"] = next_offset
        return result

    def reviewed(self):
        return {key for key, plan in self.plans.items() if key not in self.stale and "items" in plan and
                len(self.views.get(key, ())) == len(plan["items"])}

    def reject_plan(self, plan_id):
        self.stale.update(key for key, plan in self.plans.items() if plan["id"] == plan_id)

    def observed_choices(self, name, args, value):
        query = name if name in self.entities else None
        if name == "platform.result.read" and args.get("pointer") == "/items":
            query = self.entity_refs.get(args.get("resultRef"))
        if query is None or not isinstance(value, dict):
            return
        if value.get("truncated") and value.get("resultRef"):
            self.entity_refs[value["resultRef"]] = query
        for row in value.get("items", []):
            if isinstance(row, dict) and isinstance(row.get("id"), str):
                self.entities[query].add(row["id"])

    def check_choices(self, intent, upload):
        for field, (group, query) in ENTITY_FIELDS.items():
            if field not in intent:
                continue
            values = intent[field]
            if not isinstance(values, list):
                values = [values]
            original = upload.get("authorIds" if field == "authorId" else field) or []
            if isinstance(original, str):
                original = [original]
            for reference in values:
                if not isinstance(reference, str) or not reference or reference not in original and reference not in self.entities[query]:
                    raise ToolError("WORKER_ENTITY_LOOKUP_REQUIRED", f"{field} 须使用此项原选择或 {query} 已返回的稳定 ID。无需改变归属时请省略该字段。",
                                    details={"field": field, "group": group, "lookup": query})

    def ready(self, shown):
        return [number for number, row in self.by_number.items() if number in shown and row["state"] == "staged" and
                self.uploads.get(row.get("uploadId"), {}).get("researchOutcome") != "conflict" and
                not self.uploads.get(row.get("uploadId"), {}).get("upload", {}).get("replaceAssetId")]

    def selected(self, number, shown):
        allowed = {"readyInputs": self.ready(shown)}
        if isinstance(number, str) and number.isascii() and number.isdecimal() and 1 <= len(number) <= 3 and number == str(int(number)):
            number = int(number)
        if type(number) is not int:
            raise ToolError("WORKER_INPUT_SELECTOR_TYPE", "input 需要已展示的清单序号。", details=allowed)
        if number not in self.by_number:
            raise ToolError("WORKER_INPUT_OUT_OF_RANGE", "这个序号不在本批清单内。", details=allowed)
        if number not in shown:
            raise ToolError("WORKER_INPUT_NOT_DISPLAYED", "先用 task_context 的 nextOffset 读取该项。", details=allowed)
        return number, self.by_number[number]

    def staged(self, number, shown):
        _, row = self.selected(number, shown)
        if row["state"] != "staged":
            raise ToolError("WORKER_INPUT_NOT_STAGED", "这项输入不能进行研究。", details={"state": row["state"], "readyInputs": self.ready(shown)})
        item = self.uploads.get(row.get("uploadId"))
        if not item:
            raise ToolError("TASK_RESPONSE_INVALID", "现有清单缺少对应的已接收上传")
        return item["upload"]

    def bind(self, name, args, reviewed, revision, shown, page):
        # Existing full Go parameters still reach the same runtime validation.
        # Never overwrite an explicitly supplied revision, subject or fingerprint.
        self.excluded = []
        originals = name == "plan.prepare.originals"
        if originals:
            if args:
                raise ToolError("INVALID_INPUT", "保留本页原名的预览无需参数；自选项目或名称用 plan_prepare")
            args = {"items": [{"input": number} for number in page]}
        if originals or name == "plan.prepare" and "items" in args:
            if set(args) != {"items"} or not isinstance(args["items"], list):
                raise ToolError("INVALID_INPUT", "计划需要明确的逐项意图")
            items = []
            selected = set()
            ready = self.ready(shown)
            for intent in args["items"]:
                if not isinstance(intent, dict) or "input" not in intent or "uploadId" in intent:
                    raise ToolError("INVALID_INPUT", "逐项给出 input 序号和 family 基名")
                number, row = self.selected(intent["input"], shown)
                if number in selected:
                    raise ToolError("WORKER_DUPLICATE_SELECTION", "同一输入在一次命名意图中重复出现", details={"readyInputs": ready})
                selected.add(number)
                if number not in ready:
                    self.excluded.append({"input": number, "state": row["state"],
                                          "reason": row.get("errorCode") or ("requires_review" if row["state"] == "staged" else row["state"])})
                    continue
                upload = self.uploads[row["uploadId"]]["upload"]
                if originals:
                    family = upload.get("family") or PurePosixPath(row.get("originalFileName", "")).stem
                    if not family:
                        self.excluded.append({"input": number, "state": row["state"], "reason": "explicit_family_required"})
                        continue
                    intent = {"input": number, "family": family}
                self.check_choices(intent, upload)
                items.append({**{k: v for k, v in intent.items() if k != "input"}, "uploadId": upload["id"]})
            if not items:
                raise ToolError("WORKER_NO_READY_INPUT", "这次选择都不能准备计划。其他输入未被加入。", details={"readyInputs": ready, "notPlanned": self.excluded})
            return {"body": {"expectedRevision": revision, "items": items}}
        if name == "plan.prepare" and isinstance(args.get("body"), dict) and isinstance(args["body"].get("items"), list):
            for item in args["body"]["items"]:
                if isinstance(item, dict) and isinstance(item.get("uploadId"), str) and item["uploadId"] not in self.by_upload:
                    raise ToolError("WORKER_UPLOAD_NOT_IN_SELECTION", "该上传不属于本批封存清单。读取 task_context 后，可按 input 序号准备。")
        if name in {"plan.read", "plan.commit"} and "planId" not in args:
            allowed = {"plan", "offset"} if name == "plan.read" else {"plan"}
            if args.keys() - allowed:
                raise ToolError("INVALID_INPUT", "计划引用只接受当前预览的 plan")
            key = args.get("plan")
            if key is None and name == "plan.commit" and len(reviewed) == 1:
                key = next(iter(reviewed))
            if not isinstance(key, str) or key not in self.plans or (name == "plan.commit" and key not in reviewed):
                raise ToolError("WORKER_PLAN_REVIEW_REQUIRED", "请先读取所选计划的完整预览，再明确提交该计划")
            plan = self.plans[key]
            return {"planId": plan["id"], **({"body": {"fingerprint": plan["fingerprint"]}} if name == "plan.commit" else {})}
        if name in {"naming.context", "naming.suggest", "naming.preview", "collection.query"}:
            library_key = "libraryId" if name == "collection.query" else "id"
            library = self.report["batch"]["libraryId"]
            if args.get(library_key, library) != library:
                raise ToolError("WORKER_NAMING_LIBRARY_MISMATCH", "此 worker 的查询限于本批目标库，省略库参数即可。")
            args = {**args, library_key: library}
            if name == "collection.query":
                return args
            scope = args if name != "naming.preview" else args.get("body")
            if isinstance(scope, dict):
                scope = dict(scope)
                directories = {self.uploads[self.by_number[n]["uploadId"]]["upload"].get("relativeDirectory", "") for n in self.ready(shown)}
                if "directory" in scope and (not isinstance(scope["directory"], str) or scope["directory"] not in directories):
                    raise ToolError("WORKER_NAMING_DIRECTORY_MISMATCH", "指定目录与已展示的待整理输入不一致。请按清单选择目录。")
                if "directory" not in scope:
                    if len(directories) != 1:
                        raise ToolError("WORKER_NAMING_DIRECTORY_REQUIRED", "本批目录不唯一，请从已展示输入中明确选择目录。")
                    scope["directory"] = next(iter(directories))
                return {**args, "body": scope} if name == "naming.preview" else scope
        if name == "research.materials" and set(args) == {"input"}:
            upload = self.staged(args["input"], shown)
            return {"id": upload["id"], "expectedHash": upload["hash"]}
        if name == "research.submit" and "input" in args:
            if set(args) != {"input", "body"} or not isinstance(args["body"], dict) or args["body"].keys() & {"uploadId", "expectedHash", "expectedRevision"}:
                raise ToolError("INVALID_INPUT", "研究结论须绑定当前输入，不能替换其版本")
            upload = self.staged(args["input"], shown)
            return {"body": {**args["body"], "uploadId": upload["id"], "expectedHash": upload["hash"],
                             "expectedRevision": revision}}
        return args

    def tools(self, definitions):
        groups = {group for group, names in GROUPS.items() if names & definitions.keys()}
        active_plans = {key: plan for key, plan in self.plans.items() if key not in self.stale and plan["state"] in {"prepared", "partial"}}
        ready = self.ready(self.shown_inputs)
        planned = {item["uploadId"] for plan in active_plans.values() for item in plan.get("items", [])}
        pending = any(self.by_number[n].get("uploadId") not in planned for n in ready)
        active = {"task.context"}
        if ready and (pending or "planning" in self.groups):
            active.add("plan.prepare")
        if active_plans:
            active.add("plan.read")
        if self.reviewed() & active_plans.keys():
            active.add("plan.commit")
        for group in self.groups & groups:
            active |= GROUPS[group]
        if "plan.prepare" in definitions:
            groups.add("planning")
        menu = []
        if "plan.prepare" in active and "plan.prepare" in definitions and set(ready).intersection(self.page_inputs):
            menu.append({"type": "function", "function": {"name": "plan_prepare_originals",
                         "description": "保留本页待整理项各自的原名基名、已有选择和未知归属，分别准备 Go 入库预览。其他项列出原因。不会发布；预览后再决定是否采用。", "parameters": obj({})}})
        for name, definition in definitions.items():
            if name not in active:
                continue
            if name == "plan.prepare":
                function = definition["function"]
                items = function["parameters"]["properties"]["items"]
                fields = items["items"]["properties"]
                selected = {"input", "family"} | {field for field, (group, _) in ENTITY_FIELDS.items() if group in self.groups}
                if "planning" in self.groups:
                    selected.update(("groupKey", "page"))
                selected_fields = {field: value for field, value in fields.items() if field in selected}
                selected_fields["input"] = {"type": "integer", "enum": ready, "description": "选自本轮可处理的原输入序号"}
                row = obj(selected_fields, ["input", "family"])
                definition = {"type": "function", "function": {**function,
                    "parameters": obj({"items": {**items, "items": row}}, ["items"])}}
            menu.append(definition)
        menu.append({"type": "function", "function": {"name": "worker_tools",
                     "description": "需要更改归属时，authors 或 taxonomy 展开查询与稳定 ID 字段；planning 展开组图、页序与重新规划。普通命名直接用 input 和 family。",
                     "parameters": obj({"group": options(groups)}, ["group"])}})
        return menu, groups


def model_schema(name, schema):
    number = {"type": "integer", "minimum": 1, "maximum": 100, "description": "清单里原输入的 input 序号"}
    offset = {"type": "integer", "minimum": 0, "default": 0}
    plan = {"type": "string", "description": "本批清单或预览中返回的计划引用"}
    if name in {"naming.context", "naming.suggest", "naming.preview", "collection.query"}:
        bound = "libraryId" if name == "collection.query" else "id"
        schema["properties"].pop(bound, None)
        schema["required"] = [field for field in schema.get("required", []) if field != bound]
        return None
    if name == "plan.prepare":
        items = schema["properties"]["body"]["properties"]["items"]
        fields = dict(items["items"]["properties"])
        fields.pop("uploadId")
        return "逐项传清单序号 input 和命名基名 family，准备 Go 目标预览。省略归属字段会保留每项原选择及未知状态。", obj({
            "items": {**items, "items": obj({"input": number, **fields}, ["input", "family"])}}, ["items"])
    if name == "plan.read":
        return "按 plan 读 Go 的原计划。用 nextOffset 续看，直至完整预览。", obj({"plan": plan, "offset": offset}, ["plan"])
    if name == "plan.commit":
        return "确认采用已看完的这一份 Go 计划时调用。若只看完一份可直接传空对象。", obj({"plan": plan})
    if name == "task.context":
        return "重新读取本批逐项状态。用 nextOffset 查看下一页，批次修订冲突后重新核对。", obj({"offset": offset})
    if name == "research.materials":
        return "获取选定 input 当前版本的授权研究素材。", obj({"input": number}, ["input"])
    if name == "research.submit":
        body = schema["properties"]["body"]
        bound = {"expectedRevision", "uploadId", "expectedHash"}
        return "为选定 input 提交实际证据、候选或未知结论。", obj({"input": number,
            "body": {**body, "properties": {k: v for k, v in body["properties"].items() if k not in bound},
                     "required": [k for k in body["required"] if k not in bound]}}, ["input", "body"])
    return None
