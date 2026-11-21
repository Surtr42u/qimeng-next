package media.qimeng.next.data

import media.qimeng.next.model.ExtractionGroup
import media.qimeng.next.model.ExtractionLayer
import media.qimeng.next.model.ExtractionRole
import media.qimeng.next.sdk.model.SourceRuleCharacter
import media.qimeng.next.sdk.model.SourceRuleGroup
import media.qimeng.next.sdk.model.SourceRuleSettings
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * GET/PUT /sources/custom-groups 的整层读写映射锁定：
 * - 读取保留服务端生效的组、变体、角色别名、停用词、修订与重算状态；
 * - 保存恒携带完整 groups+stopWords（空数组=清空，不是省略）与读取时的修订；
 * - 非空词表经 PUT 载荷再读回不丢内容。
 */
class ExtractionVocabularyMappingTest {

    @Test fun readKeepsWholeLayerRevisionAndRebuildStatus() {
        val model = settings("completed").toExtractionVocabulary()
        assertEquals(7L, model.revision)
        assertEquals("completed", model.rebuildState)
        assertFalse(model.rebuilding)
        assertEquals(12L, model.processed)
        assertEquals(9L, model.matched)
        assertEquals(2L, model.ambiguous)
        assertNull(model.errorCode)
        val group = model.layer.groups.single()
        assertEquals("怪物猎人", group.canonical)
        assertEquals(listOf("MHW", "モンハン"), group.variants)
        assertEquals(listOf(ExtractionRole("杰玛", listOf("Gemma"))), group.roles)
        assertEquals(listOf("触手"), model.layer.stopWords)
    }

    @Test fun pendingAndRunningRebuildsStayActive() {
        assertTrue(settings("pending").toExtractionVocabulary().rebuilding)
        assertTrue(settings("running").toExtractionVocabulary().rebuilding)
        assertFalse(settings("failed").toExtractionVocabulary().rebuilding)
    }

    @Test fun failedRebuildKeepsItsErrorCode() {
        val model = settings("failed", errorCode = "rebuild_unavailable").toExtractionVocabulary()
        assertEquals("failed", model.rebuildState)
        assertEquals("rebuild_unavailable", model.errorCode)
    }

    @Test fun writeSendsTheWholeLayerWithTheReadRevision() {
        val update = layer().toSourceRuleUpdate(11L)
        assertEquals(11L, update.expectedRevision)
        assertEquals(listOf("触手"), update.stopWords)
        val group = update.groups.single()
        assertEquals("怪物猎人", group.canonical)
        assertEquals(listOf("MHW"), group.variants)
        val role = group.characters.single()
        assertEquals("杰玛", role.canonical)
        assertEquals(listOf("Gemma"), role.aliases)
    }

    @Test fun emptyLayerIsAnExplicitClearNotAnOmission() {
        val update = ExtractionLayer().toSourceRuleUpdate(3L)
        assertTrue(update.groups.isEmpty())
        assertTrue(update.stopWords.isEmpty())
        assertEquals(3L, update.expectedRevision)
    }

    @Test fun nonEmptyLayerRoundTripsThroughTheWireModels() {
        val update = layer().toSourceRuleUpdate(7L)
        val readBack = SourceRuleSettings(
            groups = update.groups,
            stopWords = update.stopWords,
            revision = 8L,
            rebuildState = sourceRuleState("pending"),
            processed = 0L,
            matched = 0L,
            ambiguous = 0L,
        ).toExtractionVocabulary()
        assertEquals(layer(), readBack.layer)
        assertEquals(8L, readBack.revision)
    }

    private fun layer() = ExtractionLayer(
        groups = listOf(
            ExtractionGroup(
                canonical = "怪物猎人",
                variants = listOf("MHW"),
                roles = listOf(ExtractionRole(canonical = "杰玛", aliases = listOf("Gemma"))),
            ),
        ),
        stopWords = listOf("触手"),
    )

    private fun settings(state: String, errorCode: String? = null) = SourceRuleSettings(
        groups = listOf(
            SourceRuleGroup(
                canonical = "怪物猎人",
                variants = listOf("MHW", "モンハン"),
                characters = listOf(SourceRuleCharacter(canonical = "杰玛", aliases = listOf("Gemma"))),
            ),
        ),
        stopWords = listOf("触手"),
        revision = 7L,
        rebuildState = sourceRuleState(state),
        processed = 12L,
        matched = 9L,
        ambiguous = 2L,
        errorCode = errorCode,
    )

    private fun sourceRuleState(state: String) = when (state) {
        "pending" -> SourceRuleSettings.RebuildState.pending
        "running" -> SourceRuleSettings.RebuildState.running
        "completed" -> SourceRuleSettings.RebuildState.completed
        "failed" -> SourceRuleSettings.RebuildState.failed
        else -> error("unexpected rebuild state $state")
    }
}
