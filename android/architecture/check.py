"""Check the effective Gradle model exported by verify.gradle and the built client APK."""

import json
from pathlib import Path
import re
import subprocess
import sys
import tempfile


REPOSITORY = Path(__file__).resolve().parents[2]
DEPENDENCIES = {
    ":app": {":runtime-api", ":core:model", ":core:data", ":core:ui"},
    ":core:data": {":sdk", ":core:model", ":core:transport"},
    ":core:ui": {":core:model"},
    ":core:model": set(),
    ":core:transport": set(),
    ":runtime-api": set(),
    ":sdk": set(),
}
EMBEDDED_ROOTS = [REPOSITORY / "android/embedded", REPOSITORY / "android/app/src/embedded"]
SDK_SOURCE = REPOSITORY / "api/generated/kotlin/src/main/kotlin"
RESTRICTED_UI = re.compile(
    r"\b(?:media\.qimeng\.next\.(?:data|sdk|transport)\b|android\.database\b|android\.content\.SharedPreferences\b|"
    r"androidx\.(?:room|sqlite|datastore)\b|java\.sql\b|retrofit2\b|okhttp3\.(?:Request|Retrofit|OkHttpClient)\b|"
    r"(?:getSharedPreferences|openOrCreateDatabase|newCall|openConnection)\s*\()"
)
# Retain newlines for file/line diagnostics. Match quoted literals before comment starts (e.g. HTTPS URLs).
LITERALS = re.compile(r'""".*?"""|"(?:\\.|[^"\\])*"|\'(?:\\.|[^\'\\])*\'|/\*.*?\*/|//[^\n]*', re.DOTALL)


def inspect_sources(facts):
    seen = set()
    counts = {}
    errors = []
    for source in facts["sources"]:
        directory = (REPOSITORY / source["dir"]).resolve()
        if any(directory.is_relative_to(folder) for folder in EMBEDDED_ROOTS):
            errors.append(f"{source['module']} {source['variant']} includes embedded {source['kind']} inputs")
        if source["kind"] not in ("kotlin", "java") or not directory.is_dir():
            continue
        module = source["module"]
        for file in directory.rglob("*"):
            if file.suffix not in (".kt", ".java") or (module, file) in seen:
                continue
            seen.add((module, file))
            counts[module] = counts.get(module, 0) + 1
            path = file.relative_to(REPOSITORY).as_posix()
            if module == ":sdk":
                if not file.is_relative_to(SDK_SOURCE):
                    errors.append(f"{path} is outside the contract generator's source root")
                continue
            raw = file.read_text(encoding="utf-8")
            clean = LITERALS.sub(lambda m: "\n" * m.group().count("\n"), raw)
            checks = []
            if module != ":core:data":
                checks.append((r"\bmedia\.qimeng\.next\.sdk\b", "generated SDK belongs in :core:data"))
            checks.append((r"\bmedia\.qimeng\.next\.embedded\b", "runtime implementation in a client source set"))
            if module == ":core:ui" or re.search(r"\bpackage\s+media\.qimeng\.next\.screen\b|\bandroidx\.compose\.runtime\.Composable\b|@Composable\b", clean):
                checks.append((RESTRICTED_UI, "presentation must use a view model or media/platform adapter"))
            if module in (":core:model", ":runtime-api", ":core:transport"):
                checks.append((r"\b(?:android\.database|androidx\.(?:room|sqlite)|java\.sql|media\.qimeng\.next\.data)\b", "client contract contains a persistence implementation"))
            for pattern, explanation in checks:
                for found in re.finditer(pattern, clean):
                    line = clean.count("\n", 0, found.start()) + 1
                    errors.append(f"{path}:{line} {explanation} ({found.group().strip()})")
    for module in DEPENDENCIES.keys() - counts.keys():
        errors.append(f"No production Kotlin/Java inputs found for {module}")
    return counts, errors


def presentation_edges(module, directories):
    blocked = re.compile(r"^(?:media\.qimeng\.next\.(?:data|sdk|transport)\.|android\.database\.|android\.content\.SharedPreferences\b|androidx\.(?:room|sqlite|datastore)\.|java\.sql\.|retrofit2\.|okhttp3\.Request\b|java\.net\.HttpURLConnection\b)")
    # jdeps reads class-file signatures/instructions. Unresolved Android runtime classes still have named targets.
    result = subprocess.run(["jdeps", "-verbose:class", "-filter:none", "--ignore-missing-deps", *directories],
                            check=True, text=True, stdout=subprocess.PIPE)
    composables = set()
    questionable = set()
    for line in result.stdout.splitlines():
        edge = re.match(r"\s+(media\.qimeng\.next\.\S+)\s+->\s+(\S+)", line)
        if edge is None:
            continue
        owner, target = edge.groups()
        if target == "androidx.compose.runtime.Composer":
            composables.add(owner)
        if blocked.search(target):
            questionable.add((owner, target))
    violations = {(owner, target) for owner, target in questionable
                  if module == ":core:ui" or owner.startswith("media.qimeng.next.screen.") or owner in composables}
    return composables, violations


def verify_return_type_example():
    # Neither presentation imports SQL. Only consuming the leaked return type creates a storage dependency.
    with tempfile.TemporaryDirectory() as temp:
        folder = Path(temp)
        port = folder / "Port.java"
        port.write_text("package media.qimeng.next.fixture; public interface Port { String title(); java.sql.Connection db(); }")
        examples = folder / "Screens.java"
        examples.write_text("""package media.qimeng.next.screen;
            import media.qimeng.next.fixture.Port;
            class ReadTitle { String title(Port port) { return port.title(); } }
            class ReadStorage { boolean status(Port port) throws Exception { return port.db().isClosed(); } }
        """)
        subprocess.run(["javac", "--release", "17", "-d", folder, port, examples], check=True)
        _, violations = presentation_edges(":app", [folder])
        if violations != {("media.qimeng.next.screen.ReadStorage", "java.sql.Connection")}:
            raise SystemExit(f"Presentation return-type regression: {violations}")


def inspect_bytecode(facts):
    errors = []
    for module in (":app", ":core:ui"):
        directories = []
        for relative in facts["compiled"].get(module, []):
            folder = REPOSITORY / relative
            if folder.is_dir() and next(folder.rglob("*.class"), None) is not None:
                directories.append(folder)
        if not directories:
            errors.append(f"No compiled production classes for {module}")
            continue
        composables, violations = presentation_edges(module, directories)
        if not composables:
            errors.append(f"No Compose classes in the actual {module} compiler outputs")
        for owner, target in violations:
            errors.append(f"{module} compiled presentation {owner} -> {target}")
    return errors


def main(inventory):
    facts = json.loads(inventory.read_text(encoding="utf-8"))
    errors = []
    # Gradle creates :core as the parent of the four code-bearing :core:* projects.
    expected_projects = set(DEPENDENCIES) | {":core"}
    projects = set(facts["projects"])
    if projects != expected_projects:
        errors.append(f"Client project set changed: unexpected={sorted(projects - expected_projects)}, "
                      f"missing={sorted(expected_projects - projects)}. Assign new modules a boundary.")
    if any(source["module"] == ":core" for source in facts["sources"]):
        errors.append("The :core parent must remain a grouping project without production sources.")
    classpaths = facts["classpaths"]
    for variant in ("clientDebug", "clientRelease"):
        for scope in ("CompileClasspath", "RuntimeClasspath"):
            if not classpaths.get(variant + scope):
                errors.append(f"Missing real project graph for {variant}{scope}")
    for name, edges in classpaths.items():
        for parent, child in edges:
            if child not in DEPENDENCIES.get(parent, set()):
                errors.append(f"{name} includes forbidden project edge {parent} -> {child}")
    counts, source_errors = inspect_sources(facts)
    errors.extend(source_errors)
    errors.extend(inspect_bytecode(facts))
    if errors:
        raise SystemExit("\n".join(sorted(set(errors))))
    verify_return_type_example()
    apk = REPOSITORY / "android/app/build/outputs/apk/client/debug/app-client-debug.apk"
    subprocess.run([sys.executable, REPOSITORY / "tools/check_android_apk.py", "client", apk], check=True)
    print(f"Checked {len(classpaths)} production classpaths and {sum(counts.values())} Java/Kotlin inputs across {len(counts)} modules.")


if __name__ == "__main__":
    main(Path(sys.argv[1]))
