"""Validate the pinned portable exporter and account for every section of its private output."""

import base64
import binascii
from collections import Counter
from datetime import date, datetime, timezone
import hashlib
import json
from pathlib import PureWindowsPath
import re


class Rejected(Exception):
    """A diagnostic containing only a fixed code, section and optional row ordinal."""


def require(condition, message):
    if not condition:
        raise Rejected(message)


def sha256(data):
    return hashlib.sha256(data).hexdigest()


def json_bytes(value):
    return (json.dumps(value, ensure_ascii=False, allow_nan=False, sort_keys=True,
                       separators=(",", ":")) + "\n").encode("utf-8")


def read_json(raw, where):
    def unique_members(pairs):
        result = {}
        for key, value in pairs:
            require(text(key), f"{where}: invalid JSON member name")
            require(key not in result, f"{where}: duplicate JSON member")
            result[key] = value
        return result

    def invalid_constant(_):
        raise Rejected(f"{where}: invalid JSON number")

    try:
        return json.loads(raw.decode("utf-8"), object_pairs_hook=unique_members,
                          parse_constant=invalid_constant)
    except (ValueError, RecursionError) as error:
        raise Rejected(f"{where}: invalid UTF-8 JSON") from error


# The current Go exporter emits each of these, including empty arrays. Its sqlc
# followed-author query is the one slice that serializes as null when empty.
SECTIONS = {
    "mediaFiles": "identity_and_selected_original_verified",
    "viewEvents": "exported_event_identity_and_reference_verified",
    "authors": "exported_author_identity_verified",
    "authorMediaRefs": "exported_author_asset_reference_verified",
    "tags": "exported_vocabulary_verified",
    "mediaTagRefs": "exported_tag_asset_reference_verified",
    "favorites": "exported_membership_verified_time_unavailable",
    "followedAuthorIds": "exported_membership_verified_time_unavailable",
    "timelineTags": "exported_marker_reference_verified_source_id_and_version_unavailable",
    "likes": "exported_aggregate_verified_individual_days_unavailable",
    "history": "exported_derived_rows_sealed_no_event_replay",
    "mediaStats": "exported_derived_rows_sealed_no_event_replay",
    "dailyBrowse": "exported_derived_rows_sealed_no_event_replay",
    "txtFragments": "exported_text_sealed_verbatim",
    "authorMasterTxt": "exported_generated_text_sealed_verbatim",
    "appPrefs": "exported_preferences_sealed",
    "settings": "sealed_only",
    "scanSources": "sealed_only",
    "albumRules": "sealed_only",
    "cosWorks": "sealed_only",
}

NOT_EXPORTED = [
    "old_instance_id", "old_library_ids_and_registration_history",
    "empty_library_inventory_and_library_enablement",
    "original_content_hash_at_export_and_content_versions",
    "trash_files_and_deletion_history", "playback_resume_and_unexported_mark_ids",
    "individual_like_rows_and_favorite_times", "unexported_asset_and_relation_fields",
    "events_without_exported_media", "unexported_runtime_settings_and_audit_rows",
    "upload_and_maintenance_records", "device_operations_and_credentials",
]

MEDIA_TYPES = {
    ".jpg": "image", ".jpeg": "image", ".png": "image", ".webp": "image", ".avif": "image",
    ".gif": "animated_image", ".mp4": "video", ".mkv": "video", ".webm": "video",
    ".mov": "video", ".m4v": "video", ".avi": "video",
}
STAMP = re.compile(r"\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d{1,9})?(?:Z|[+-]\d\d:\d\d)", re.ASCII)


def number(value):
    return type(value) is int and -(1 << 63) <= value < (1 << 63)


def text(value):
    if not isinstance(value, str) or "\x00" in value:
        return False
    try:
        value.encode("utf-8")
    except UnicodeEncodeError:
        return False
    return True


def identity(value):
    return text(value) and value != ""


def safe_rel(value):
    if not identity(value) or any(char in value for char in '\\:<>|"*?'):
        return False
    parts = value.split("/")
    return all(part not in {"", ".", ".."} and not part.endswith((".", " "))
               and not PureWindowsPath(part).is_reserved()
               and all(ord(char) >= 32 for char in part) for part in parts)


def valid_day(value):
    if not isinstance(value, str) or not re.fullmatch(r"\d{4}-\d\d-\d\d", value, re.ASCII):
        return False
    try:
        date.fromisoformat(value)
    except ValueError:
        return False
    return True


def event_time(value, where):
    require(isinstance(value, str) and STAMP.fullmatch(value), f"{where}: invalid timestamp")
    try:
        instant = datetime.fromisoformat(value).astimezone(timezone.utc)
    except (ValueError, OverflowError) as error:
        raise Rejected(f"{where}: invalid timestamp") from error
    require(instant != datetime.min.replace(tzinfo=timezone.utc), f"{where}: zero event time")
    # Integer arithmetic preserves the exporter's UnixMilli convention, including before 1970.
    elapsed = instant - datetime(1970, 1, 1, tzinfo=timezone.utc)
    return (elapsed.days * 86400 + elapsed.seconds) * 1000 + elapsed.microseconds // 1000


def unique(seen, key, where):
    require(key not in seen, f"{where}: duplicate identity")
    seen.add(key)


def rows(data, section):
    for index, row in enumerate(data[section] or []):
        where = f"{section}[{index}]"
        require(isinstance(row, dict), f"{where}: expected an object")
        yield row, where


def validate_media(data):
    assets, record_keys, paths = set(), {}, set()
    for row, where in rows(data, "mediaFiles"):
        for key in ("assetId", "recordKey", "fileName", "libraryName", "relPath"):
            require(identity(row.get(key)), f"{where}: missing {key}")
        rel = row["relPath"]
        require(safe_rel(rel) and rel.split("/")[-1] == row["fileName"],
                f"{where}: unsafe or inconsistent relative path")
        suffix = "." + row["fileName"].rsplit(".", 1)[-1].lower()
        require(suffix in MEDIA_TYPES and MEDIA_TYPES[suffix] == row.get("mediaType"),
                f"{where}: media type and extension differ")
        require(number(row.get("sizeBytes")) and row["sizeBytes"] >= 0
                and number(row.get("modifiedAtMillis")), f"{where}: invalid size or mtime")
        for key in ("indexedAtMillis", "tagsUpdatedAtMillis", "width", "height", "durationMillis"):
            require(row.get(key) is None or number(row[key]), f"{where}: invalid {key}")
        unique(assets, row["assetId"], where)
        unique(paths, (row["libraryName"], rel.casefold()), where)
        require(row["recordKey"] not in record_keys, f"{where}: duplicate recordKey")
        record_keys[row["recordKey"]] = row
    return record_keys


def validate_authors(data):
    authors = set()
    for row, where in rows(data, "authors"):
        require(identity(row.get("authorId")) and identity(row.get("displayName")),
                f"{where}: missing author identity")
        unique(authors, row["authorId"], where)
        if "profileJson" in row:
            require(text(row["profileJson"]), f"{where}: invalid profileJson")
            profile = read_json(row["profileJson"].encode(), where)
            require(isinstance(profile, dict) and profile.get("authorId") == row["authorId"],
                    f"{where}: profile identity differs")
        if "avatarData" in row:
            require("profileJson" in row and isinstance(row["avatarData"], str),
                    f"{where}: avatar without profile")
            require(identity(profile.get("avatarUrl")), f"{where}: avatar without exported source")
            try:
                avatar = base64.b64decode(row["avatarData"], validate=True)
            except (ValueError, binascii.Error) as error:
                raise Rejected(f"{where}: invalid exported avatar") from error
            require(0 < len(avatar) <= 2 << 20, f"{where}: invalid exported avatar length")
            require(avatar.startswith((b"\x89PNG\r\n\x1a\n", b"\xff\xd8\xff", b"GIF87a", b"GIF89a"))
                    or avatar[:4] == b"RIFF" and avatar[8:12] == b"WEBP",
                    f"{where}: unsupported exported avatar bytes")
    return authors


def media_reference(row, where, media):
    key = row.get("recordKey")
    require(identity(key) and key in media, f"{where}: unresolved recordKey")
    if "fileName" in row:
        require(row["fileName"] == media[key]["fileName"], f"{where}: filename differs")
    if "mediaType" in row:
        require(row["mediaType"] == media[key]["mediaType"], f"{where}: media type differs")
    return key


def validate_events(data, media):
    events, opens = set(), Counter()
    for row, where in rows(data, "viewEvents"):
        key = media_reference(row, where, media)
        require(identity(row.get("eventId")), f"{where}: missing eventId")
        unique(events, row["eventId"], where)
        require(row.get("kind") in ("open", "play", "dwell") and text(row.get("sessionId")),
                f"{where}: invalid event kind or session")
        require("seconds" in row and (row["seconds"] is None or
                number(row["seconds"]) and row["seconds"] >= 0), f"{where}: invalid seconds")
        require("day" not in row or valid_day(row["day"]), f"{where}: invalid day")
        millis = event_time(row.get("startedAt"), where)
        if row["kind"] == "open":
            opens[key, millis] += 1
    history = Counter()
    for row, where in rows(data, "history"):
        key = media_reference(row, where, media)
        require(number(row.get("openedAtMillis")) and "fileName" in row and "mediaType" in row,
                f"{where}: invalid history row")
        history[key, row["openedAtMillis"]] += 1
    require(history == opens, "history: open events and exported history differ")


def validate_relations(data, media, authors):
    tags = set()
    for row, where in rows(data, "tags"):
        require(identity(row.get("name")), f"{where}: missing name")
        unique(tags, row["name"], where)
    for section, targets in (("favorites", media), ("followedAuthorIds", authors)):
        seen = set()
        for index, key in enumerate(data[section] or []):
            where = f"{section}[{index}]"
            require(identity(key) and key in targets, f"{where}: unresolved identity")
            unique(seen, key, where)
    for section, field, targets in (("authorMediaRefs", "authorId", authors),
                                    ("mediaTagRefs", "tagName", tags)):
        seen = set()
        for row, where in rows(data, section):
            key = media_reference(row, where, media)
            require(identity(row.get(field)) and row[field] in targets, f"{where}: unresolved {field}")
            require(section != "authorMediaRefs" or "fileName" in row, f"{where}: missing fileName")
            unique(seen, (key, row[field]), where)
    for section in ("timelineTags", "likes", "dailyBrowse", "mediaStats"):
        seen = set()
        for row, where in rows(data, section):
            key = media_reference(row, where, media)
            if section == "timelineTags":
                require("fileName" in row and identity(row.get("name")) and number(row.get("timeMillis"))
                        and row["timeMillis"] >= 0, f"{where}: invalid marker")
            elif section == "likes":
                unique(seen, key, where)
                require(number(row.get("likeCount")) and row["likeCount"] >= 0
                        and ("lastLikeDate" not in row or valid_day(row["lastLikeDate"])),
                        f"{where}: invalid like aggregate")
            elif section == "dailyBrowse":
                require("fileName" in row and "mediaType" in row and number(row.get("dayStartMillis")),
                        f"{where}: invalid day aggregate")
                unique(seen, (key, row["dayStartMillis"]), where)
            else:
                require("fileName" in row, f"{where}: missing fileName")
                unique(seen, key, where)


def validate_export(raw):
    document = read_json(raw, "export")
    require(isinstance(document, dict) and document.get("format") == "qimeng_backup"
            and document.get("appIdentifier") == "com.qimeng.media"
            and type(document.get("schemaVersion")) is int and document["schemaVersion"] == 2,
            "export: unsupported envelope")
    require(number(document.get("exportedAtMillis")), "export: missing export time")
    data = document.get("data")
    require(isinstance(data, dict), "export: missing data")
    for section in SECTIONS:
        if section == "appPrefs":
            require(section not in data or isinstance(data[section], dict), "appPrefs: expected object")
        elif section == "authorMasterTxt":
            require(text(data.get(section)), "authorMasterTxt: expected exported text")
        else:
            require(section in data and (isinstance(data[section], list) or
                    section == "followedAuthorIds" and data[section] is None),
                    f"{section}: complete exporter section required")
    media = validate_media(data)
    authors = validate_authors(data)
    validate_events(data, media)
    validate_relations(data, media, authors)
    fragments = set()
    for row, where in rows(data, "txtFragments"):
        require(identity(row.get("filename")) and text(row.get("content"))
                and number(row.get("importedAtMillis")), f"{where}: invalid text fragment")
        unique(fragments, row["filename"], where)
    for section in ("settings", "scanSources", "albumRules", "cosWorks"):
        require(all(isinstance(row, dict) for row in data[section]),
                f"{section}: expected object rows for private retention")
    return document


def coverage(document, digest, inventory):
    data = document["data"]
    sections = {}
    for section in sorted(SECTIONS.keys() | data.keys()):
        if section not in data:
            sections[section] = {"disposition": "not_present_in_export"}
            continue
        value = data[section]
        state = {"disposition": SECTIONS.get(section, "unknown_section_sealed_verbatim")}
        if isinstance(value, list):
            state["rows"] = len(value)
        elif value is None:
            state["representation"] = "null"
            if section == "followedAuthorIds":
                state["rows"] = 0
        else:
            state["values"] = 1
        sections[section] = state
    return {
        "format": "qimeng-mobile-export-coverage/v1",
        "scope": "exported_facts_only",
        "sourceId": "portable-v2:" + digest,
        "oldInstanceId": None, "oldLibraryIds": None,
        "exportedAtMillis": document["exportedAtMillis"],
        "rawEvidence": "export.json",
        "absoluteRootPaths": "operator_private_mapping",
        "originals": {"hashOrigin": "selected_original_bytes_at_preparation",
                      "exportedContentHashes": False, **inventory},
        "sections": sections,
        "extraEnvelopeMembersSealed": sorted(document.keys() - {
            "format", "schemaVersion", "appIdentifier", "exportedAtMillis", "data"}),
        "allFieldValuesPreservedInRawExport": True,
        "notRecoverableFromThisExport": NOT_EXPORTED,
        "nativeArchiveCreated": False,
    }
