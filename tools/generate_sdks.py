"""仅在 CI 从唯一 OpenAPI 生成客户端、Go 路由及来源锁。"""
from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import urllib.error
import urllib.request
import zipfile

import yaml

ROOT = Path(__file__).resolve().parents[1]
OPENAPI_GENERATOR = "7.27.0"
OPENAPI_JAR_SHA256 = "db9c4b738727698748746144394cb7ee96512fe50166555baaeeca9311ee1dba"
OAPI_CODEGEN = "v2.8.2"


class UniqueSafeLoader(yaml.SafeLoader):
    """Duplicate paths/schemas must fail instead of silently replacing a contract."""


def unique_mapping(loader, node, deep=False):
    result = {}
    for key_node, value_node in node.value:
        key = loader.construct_object(key_node, deep=deep)
        if key in result:
            raise ValueError(f"Duplicate OpenAPI key at line {key_node.start_mark.line + 1}: {key}")
        result[key] = loader.construct_object(value_node, deep=deep)
    return result


UniqueSafeLoader.add_constructor(yaml.resolver.BaseResolver.DEFAULT_MAPPING_TAG, unique_mapping)


def read_specification():
    document = yaml.load((ROOT / "api/openapi.yaml").read_bytes(), Loader=UniqueSafeLoader)
    def inspect(value, path="openapi"):
        if isinstance(value, dict):
            for key, child in value.items():
                if key in {"example", "examples", "default"}:
                    continue
                if child is None:
                    raise ValueError(f"Empty OpenAPI mapping at {path}.{key}; quote inline descriptions containing commas")
                inspect(child, f"{path}.{key}")
        elif isinstance(value, list):
            for index, child in enumerate(value):
                inspect(child, f"{path}[{index}]")
    inspect(document)
    return document


def run(*args: str, cwd: Path = ROOT) -> None:
    subprocess.run(args, cwd=cwd, check=True)


def kotlin_generator(cache: Path) -> Path:
    name = f"openapi-generator-cli-{OPENAPI_GENERATOR}.jar"
    jar = cache / name
    if jar.is_file():
        content = jar.read_bytes()
    else:
        coordinate = f"org/openapitools/openapi-generator-cli/{OPENAPI_GENERATOR}/{name}"
        repositories = ("https://repo.maven.apache.org/maven2", "https://maven-central.storage-download.googleapis.com/maven2")
        for repository in repositories:
            try:
                with urllib.request.urlopen(f"{repository}/{coordinate}", timeout=120) as response:
                    content = response.read(100 * 1024 * 1024)
                break
            except urllib.error.HTTPError as error:
                if repository == repositories[-1] or error.code not in (429, 502, 503, 504):
                    raise
                print(f"Maven Central returned {error.code}. Using its mirror for the pinned artifact.")
    if hashlib.sha256(content).hexdigest() != OPENAPI_JAR_SHA256:
        raise RuntimeError("OpenAPI generator checksum mismatch")
    if not jar.exists():
        jar.write_bytes(content)
    return jar


def kotlin() -> None:
    cache = ROOT / "build" / "toolchain"
    cache.mkdir(parents=True, exist_ok=True)
    jar = kotlin_generator(cache)
    # Retrofit buffers an unannotated ResponseBody before handing it to its
    # caller. Render every binary response as a stream directly in the pinned
    # generator's template, including portable archives larger than phone RAM.
    templates = cache / "kotlin-templates"
    relative = Path("libraries/jvm-retrofit2/api.mustache")
    with zipfile.ZipFile(jar) as archive:
        template = archive.read("kotlin-client/" + relative.as_posix()).decode("utf-8")
    anchor = "    {{#isDeprecated}}\n"
    if template.count(anchor) != 1:
        raise RuntimeError("The upstream Retrofit operation template changed.")
    customized = templates / relative
    customized.parent.mkdir(parents=True, exist_ok=True)
    customized.write_text(template.replace(anchor, "    {{#isResponseFile}}\n    @Streaming\n    {{/isResponseFile}}\n" + anchor), encoding="utf-8")
    run("java", "-jar", str(jar), "generate", "-i", "api/openapi.yaml", "-g", "kotlin",
        "--library", "jvm-retrofit2", "-o", "api/generated/kotlin",
        "--template-dir", str(templates),
        "--api-package", "media.qimeng.next.sdk.api",
        "--model-package", "media.qimeng.next.sdk.model",
        "--invoker-package", "media.qimeng.next.sdk.infrastructure",
        "--type-mappings", "file=RequestBody",
        "--import-mappings", "RequestBody=okhttp3.RequestBody",
        "--enum-name-mappings", "name=nameValue",
        "--additional-properties", "serializationLibrary=gson,dateLibrary=string,useCoroutines=true,useResponseAsReturnType=true,packageName=media.qimeng.next.sdk,hideGenerationTimestamp=true,enumPropertyNaming=original",
        "--global-property", "apiTests=false,modelTests=false,apiDocs=false,modelDocs=false")
    interface = ROOT / "api/generated/kotlin/src/main/kotlin/media/qimeng/next/sdk/api/DefaultApi.kt"
    generated = interface.read_text(encoding="utf-8")
    for operation in ("downloadBackupArchive", "exportAuthorList", "exportBackupAuthorDocuments", "exportMasterAuthorList", "getDeviceApplySegment"):
        signature = "suspend fun " + operation + "("
        start = generated.index(signature)
        annotations = generated[generated.rfind("*/", 0, start):start]
        if "@Streaming" not in annotations or "Response<ResponseBody>" not in generated[start:generated.index("\n", start)]:
            raise RuntimeError("Expected a streaming response for " + operation)


def go() -> None:
    destination = ROOT / "server/internal/transport/generated"
    destination.mkdir(parents=True, exist_ok=True)
    run("go", "run", f"github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@{OAPI_CODEGEN}",
        "-generate", "types,client,std-http-server", "-package", "generated", "-o", str(destination / "api.gen.go"),
        str(ROOT / "api/openapi.yaml"), cwd=ROOT / "server")
    go_request_rules(destination, read_specification())


def go_request_rules(destination: Path, document: dict) -> None:
    """Extra request information the standard server binder leaves to handlers."""
    def object_fields(schema):
        required = set(schema.get("required", []))
        properties = dict(schema.get("properties", {}))
        closed = schema.get("additionalProperties") is False
        for part in schema.get("allOf", []):
            inherited, fields, sealed = object_fields(part)
            required.update(inherited)
            properties.update(fields)
            closed |= sealed
        return required, properties, closed

    def string_slice(names):
        return "[]string{" + ", ".join(json.dumps(name) for name in sorted(names)) + "}"

    lines = ["// Code generated from api/openapi.yaml. DO NOT EDIT.", "package generated", "",
             "type HTTPRequestRule struct {", "Public, BodyRequired, Object bool",
             "Required, Nullable, Allowed []string", "}", "",
             "var HTTPRequestRules = map[string]HTTPRequestRule{"]
    base = document["servers"][0]["url"]
    for path, item in sorted(document["paths"].items()):
        for method, operation in sorted(item.items()):
            if method not in {"get", "put", "post", "patch", "delete", "head"}:
                continue
            fields = []
            if operation.get("security", document.get("security")) == []:
                fields.append("Public: true")
            body = resolve(document, operation.get("requestBody", {}))
            content = body.get("content", {}).get("application/json")
            if content:
                schema = content["schema"]
                required, properties, closed = object_fields(schema)
                if body.get("required"):
                    fields.append("BodyRequired: true")
                if schema.get("type") == "object" or properties or required:
                    fields.append("Object: true")
                if required:
                    fields.append("Required: " + string_slice(required))
                    nullable = [name for name in required if "null" in properties.get(name, {}).get("type", [])]
                    if nullable:
                        fields.append("Nullable: " + string_slice(nullable))
                if closed:
                    fields.append("Allowed: " + string_slice(properties))
            if fields:
                pattern = method.upper() + " " + base + path
                lines.append(json.dumps(pattern) + ": {" + ", ".join(fields) + "},")
    output = destination / "request_rules.gen.go"
    output.write_text("\n".join(lines + ["}", ""]), encoding="utf-8")
    run("gofmt", "-w", str(output))


def resolve(document: dict, value, stack: tuple[str, ...] = ()):
    if isinstance(value, list):
        return [resolve(document, child, stack) for child in value]
    if not isinstance(value, dict):
        return value
    if "$ref" in value:
        reference = value["$ref"]
        if not reference.startswith("#/") or reference in stack:
            raise ValueError(f"Unsupported external or recursive tool schema: {reference}")
        node = document
        for part in reference[2:].split("/"):
            node = node[part.replace("~1", "/").replace("~0", "~")]
        return resolve(document, {**node, **{k: v for k, v in value.items() if k != "$ref"}}, stack + (reference,))
    output = {key: resolve(document, child, stack) for key, child in value.items() if key != "nullable"}
    if value.get("nullable") and isinstance(output.get("type"), str):
        output["type"] = [output["type"], "null"]
    return output


def python_contract() -> None:
    """生成操作路由、参数和 schema；AI 只调用显式声明的 x-ai-tool 操作。"""
    raw = (ROOT / "api/openapi.yaml").read_bytes()
    document = read_specification()
    tool_map = json.loads((ROOT / "ai/tool-map.json").read_text(encoding="utf-8"))
    operations = {}
    for path, item in document["paths"].items():
        for method, operation in item.items():
            if method not in {"get", "post", "put", "patch", "delete", "head"}:
                continue
            identifier = operation["operationId"]
            parameters = [resolve(document, value) for value in item.get("parameters", []) + operation.get("parameters", [])]
            properties, required = {}, []
            for parameter in parameters:
                name = parameter["name"]
                properties[name] = parameter.get("schema", {})
                if parameter.get("required"):
                    required.append(name)
            body = resolve(document, operation.get("requestBody", {}))
            content = body.get("content", {})
            if "application/json" in content:
                properties["body"] = content["application/json"].get("schema", {})
                if body.get("required"):
                    required.append("body")
            operations[identifier] = {
                "method": method.upper(), "path": path, "parameters": parameters,
                "body": "application/json" if "application/json" in content else next(iter(content), None),
                "description": operation.get("description", operation.get("summary", identifier)),
                "tool": operation.get("x-ai-tool", tool_map.get(identifier, {}).get("tool")),
                "scope": operation.get("x-ai-scope", tool_map.get(identifier, {}).get("scope", "read")),
                "readOnly": operation.get("x-ai-read-only", tool_map.get(identifier, {}).get("readOnly", method in {"get", "head"})),
                "inputSchema": {"type": "object", "properties": properties,
                                "required": required, "additionalProperties": False},
            }
    target = ROOT / "api/generated/python/operations.json"
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text(json.dumps({"openapiSha256": hashlib.sha256(raw).hexdigest(),
                                 "apiVersion": document["info"]["version"],
                                 "basePath": document["servers"][0]["url"], "operations": operations},
                                ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


def source_lock() -> None:
    directories = ("api/generated/kotlin/src/main/kotlin", "api/generated/python",
                   "server/internal/transport/generated", "web/src/api/generated")
    files = {}
    for directory in directories:
        path = ROOT / directory
        if not path.is_dir():
            raise RuntimeError(f"Missing SDK output: {directory}")
        for child in sorted(path.rglob("*")):
            if child.is_file():
                files[child.relative_to(ROOT).as_posix()] = hashlib.sha256(child.read_bytes()).hexdigest()
    lock = {"schemaVersion": 1, "openapiSha256": hashlib.sha256((ROOT / "api/openapi.yaml").read_bytes()).hexdigest(),
            "generators": {"kotlin": OPENAPI_GENERATOR, "go": OAPI_CODEGEN,
                           "typescript": "web/package-lock.json", "python": "tools/generate_sdks.py"}, "files": files}
    (ROOT / "api/sdk.lock").write_text(json.dumps(lock, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


def main() -> None:
    if os.environ.get("CI") != "true":
        raise SystemExit("SDK generation is cloud-only; run the GitHub Actions workflow.")
    parser = argparse.ArgumentParser()
    parser.add_argument("--target", choices=["all", "go", "typescript", "kotlin", "python", "lock"], default="all")
    target = parser.parse_args().target
    read_specification()
    actions = {"go": go, "typescript": lambda: run("npm", "run", "generate:api", cwd=ROOT / "web"),
               "kotlin": kotlin, "python": python_contract, "lock": source_lock}
    for name, action in actions.items():
        if target in ("all", name):
            action()


if __name__ == "__main__":
    main()
