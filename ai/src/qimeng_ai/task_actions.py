"""Present Go batch transitions as tools that this executor can actually call."""
from .errors import ToolError


def present_batch_actions(runtime, context, tools):
    hints = context["nextActions"]
    visible = {tool["name"]: tool for tool in tools}
    actions, waits = [], []

    def wait(reason, **details):
        item = {"reason": reason, **details}
        if item not in waits:
            waits.append(item)

    def page_inputs(state):
        return [row["id"] for row in context["inputs"] if row["state"] == state]

    session = None
    if hints:
        try:
            session = runtime.transport.call("getCurrentSession", {})
        except ToolError as error:
            wait("session_unavailable", code=error.code)

    for hint in hints:
        if hint == "awaitUserDuplicateDecision":
            wait("user_duplicate_decision", inputIds=page_inputs("duplicate"))
            continue
        if hint in {"appendUpload", "completeUpload"}:
            # The original sender owns its byte stream and completion key.
            # Reading the received prefix needs neither a fresh fileRef nor a new transfer.
            wait("original_sender_transfer", inputIds=page_inputs("receiving"))
            hint = "getUpload"
        name = runtime.contract["operations"].get(hint, {}).get("tool") or hint
        tool = visible.get(name)
        if tool is None:
            wait("unavailable_tool", operation=hint)
            continue
        if session is None:
            continue
        scope = tool["scope"]
        local = {scope}
        if name in {"intake.register", "upload.file", "source.recycle"}:
            local.add("local.read")
        if name == "source.recycle":
            local.add("local.recycle")
        missing = sorted(local - runtime.config.permissions)
        scopes = session["scopes"]
        allowed = "admin" in scopes or scope in scopes
        bound = "admin" in scopes or scope == "read" or session.get("batchId") == context["batch"]["id"]
        if missing or not allowed or not bound:
            wait("permission_required", tool=name, missingLocalScopes=missing,
                 missingServerScopes=[] if allowed else [scope], requiresBatchBinding=not bound)
            continue
        if name in {"intake.register", "upload.file"} and not runtime.config.storages:
            wait("original_sender_selection", inputIds=page_inputs("pending"))
            continue
        if name == "source.recycle" and not runtime.config.recycle_storages:
            wait("source_recycle_not_configured")
            continue
        if name not in actions:
            actions.append(name)
    return {**context, "nextActions": actions, "actionWaits": waits}
