"""Only the owner-targeted instrumentation entry may be present in the companion."""
import sys
import xml.etree.ElementTree as ET

manifest = ET.parse(sys.stdin).getroot()
android = "{http://schemas.android.com/apk/res/android}"
target = "media.qimeng.next"
assert manifest.get("package") == target + ".test"
entries = manifest.findall("instrumentation")
assert len(entries) == 1
entry = entries[0]
assert entry.get(android + "name") == target + ".test.MigrationAcceptance"
assert entry.get(android + "targetPackage") == entry.get(android + "targetProcesses") == target
app = manifest.find("application")
assert app is not None and app.get(android + "allowBackup") == "false"
assert all(app.find(tag) is None for tag in ("activity", "activity-alias", "service", "provider", "receiver"))
assert not manifest.findall("uses-permission")
