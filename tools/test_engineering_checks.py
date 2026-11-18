from pathlib import Path
import tempfile
import unittest

from check_docs import link_errors
from check_doc_impact import evaluate
from check_migration_history import changes


class EngineeringChecksTest(unittest.TestCase):
    def test_document_impact_uses_the_changed_domain(self):
        paths = {"api/openapi.yaml", "server/internal/architecture/boundaries_test.go",
                 "server/internal/store/migrations/0042_example.sql", "docs/api.md"}
        _, missing = evaluate(paths, "文档无需更新[模块边界] 本次只调整测试的错误信息。")
        self.assertEqual(missing, ["数据迁移"])
        self.assertEqual(evaluate(paths | {"docs/migration.md", "docs/architecture.md"}, "")[1], [])
        self.assertEqual(evaluate({"server/internal/service/migration_profile_test.go"}, ""), ([], []))
        self.assertEqual(evaluate({"server/internal/service/native_delivery_commit.go"}, "")[1], ["数据迁移"])
        self.assertEqual(evaluate({"server/internal/service/native_delivery_commit.go", "docs/migration.md"}, "")[1], [])

    def test_document_references(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory).resolve()
            (root / "规则.md").write_text("# 架构边界\n## 复核\n## 复核\n", encoding="utf-8")
            (root / "README.md").write_text(
                "[正文](规则.md#架构边界) [第二项](规则.md#复核-1) [旧目录](规则.md#不存在) "
                "[越界](../outside.md)\n```md\n[示例](missing.md)\n```\n",
                encoding="utf-8",
            )
            errors = link_errors(root, ["README.md"])
            self.assertEqual(len(errors), 2)
            self.assertTrue(any("不存在" in error for error in errors))
            self.assertTrue(any("outside.md" in error for error in errors))

    def test_history_is_immutable_but_historical_gaps_can_remain(self):
        before = {"0001_start.sql": "a", "0003_added.sql": "b"}
        self.assertEqual(changes(before, {**before, "0004_next.sql": "c"}), [])
        for after in (
            {"0003_added.sql": "b"},
            {**before, "0001_start.sql": "changed"},
            {**before, "0002_backfill.sql": "c"},
            {**before, "0004_first.sql": "c", "0004_second.sql": "d"},
        ):
            with self.subTest(after=after):
                self.assertTrue(changes(before, after))
