package media.qimeng.next

import media.qimeng.next.model.ExtractionGroup
import media.qimeng.next.model.ExtractionLayer
import media.qimeng.next.model.ExtractionRole
import media.qimeng.next.model.ExtractionVocabulary
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * 整组词表编辑的读取合并规则锁定：干净编辑采用服务端生效词表；未保存草稿在同修订回读时
 * 只换基准、不被覆盖；服务端修订已变时保留草稿并标记冲突；明确重读才替换草稿。
 */
class ExtractionEditorRulesTest {

    @Test fun cleanEditorAdoptsTheServerVocabulary() {
        val server = vocabulary(3, "怪物猎人")
        val read = ExtractionEditorState(base = vocabulary(2, "旧组"), draft = vocabulary(2, "旧组").layer)
            .afterRead(server, replaceDraft = false)
        assertEquals(server, read.base)
        assertEquals(server.layer, read.draft)
        assertFalse(read.conflict)
        assertFalse(read.loading)
    }

    @Test fun dirtyDraftSurvivesAReadAtTheSameRevision() {
        val base = vocabulary(3, "怪物猎人")
        val draft = base.layer.copy(groups = base.layer.groups + ExtractionGroup(canonical = "新组"))
        val read = ExtractionEditorState(base = base, draft = draft).afterRead(base, replaceDraft = false)
        assertEquals(draft, read.draft)
        assertEquals(base, read.base)
        assertFalse(read.conflict)
    }

    @Test fun serverRevisionChangeKeepsTheDraftAndFlagsConflict() {
        val base = vocabulary(3, "怪物猎人")
        val draft = base.layer.copy(stopWords = base.layer.stopWords + "保留草稿")
        val newer = vocabulary(4, "服务端新组")
        val read = ExtractionEditorState(base = base, draft = draft).afterRead(newer, replaceDraft = false)
        assertEquals(draft, read.draft)
        assertEquals(base, read.base)
        assertTrue(read.conflict)
    }

    @Test fun explicitReloadReplacesTheDraft() {
        val base = vocabulary(3, "怪物猎人")
        val draft = base.layer.copy(stopWords = base.layer.stopWords + "将被放弃")
        val newer = vocabulary(4, "服务端新组")
        val read = ExtractionEditorState(base = base, draft = draft).afterRead(newer, replaceDraft = true)
        assertEquals(newer, read.base)
        assertEquals(newer.layer, read.draft)
        assertFalse(read.conflict)
    }

    private fun vocabulary(revision: Long, group: String) = ExtractionVocabulary(
        layer = ExtractionLayer(
            groups = listOf(
                ExtractionGroup(
                    canonical = group,
                    variants = listOf("变体"),
                    roles = listOf(ExtractionRole(canonical = "角色", aliases = listOf("别名"))),
                ),
            ),
            stopWords = listOf("触手"),
        ),
        revision = revision,
        rebuildState = "completed",
        processed = 1L,
        matched = 1L,
        ambiguous = 0L,
        errorCode = null,
    )
}
