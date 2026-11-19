package media.qimeng.next.test

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
import media.qimeng.next.ThumbnailCachePage
import media.qimeng.next.model.ThumbnailCacheSelection
import media.qimeng.next.model.ThumbnailCoverage
import media.qimeng.next.model.ThumbnailPool
import media.qimeng.next.model.ThumbnailPoolUsage
import media.qimeng.next.model.ThumbnailPrefetch
import media.qimeng.next.model.ThumbnailPrefetchStatus
import media.qimeng.next.model.ThumbnailUsage
import media.qimeng.next.screen.ThumbnailCacheContent
import media.qimeng.next.testsupport.captureCloudScreenshot
import media.qimeng.next.ui.QimengTheme
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test

class ThumbnailCacheScreenTest {
    @get:Rule val compose = createComposeRule()

    @Test fun eachCardClearsAfterItsOwnConfirmationAndKeepsItsOwnCounters() {
        val nas = ThumbnailCacheSelection(ThumbnailPool.NAS, "nas-fixture")
        val local = ThumbnailCacheSelection(ThumbnailPool.LOCAL, "local-fixture")
        val cleared = mutableListOf<ThumbnailCacheSelection>()
        var page by mutableStateOf(ThumbnailCachePage(
            nas = ThumbnailPoolUsage(nas, ThumbnailUsage(2, 2048)),
            local = ThumbnailPoolUsage(local, ThumbnailUsage(3, 4096)),
            coverage = local to ThumbnailCoverage(7, 9),
        ))
        val prefetch = ThumbnailPrefetch(local, ThumbnailPrefetchStatus.Running(3, 9))
        compose.setContent {
            QimengTheme(false) {
                ThumbnailCacheContent(page, prefetch, onClear = { selection ->
                    cleared += selection
                    val empty = ThumbnailPoolUsage(selection, ThumbnailUsage(0, 0))
                    page = if (selection.pool == ThumbnailPool.NAS) page.copy(nas = empty) else page.copy(local = empty)
                }, onBack = {})
            }
        }
        compose.onNodeWithText("缩略图缓存").assertIsDisplayed()
        compose.onAllNodesWithText("文件数").assertCountEquals(2)
        compose.onAllNodesWithText("实际占用").assertCountEquals(2)
        compose.onNodeWithText("2 个").assertExists()
        compose.onNodeWithText("3 个").assertExists()
        compose.onNodeWithText("生成进度").assertExists()
        compose.onNodeWithText("已生成 7 / 9", substring = true).assertExists()
        compose.onNodeWithText("已缓存 3 / 9", substring = true).assertExists()
        compose.onAllNodesWithText("预取").assertCountEquals(1)
        assertTrue(captureCloudScreenshot("next-thumbnail-cache.png"))

        compose.onNodeWithText("清空服务器缓存").performScrollTo().performClick()
        compose.onNodeWithText("清空服务器缓存？").assertIsDisplayed()
        compose.onNodeWithText("取消").performClick()
        compose.runOnIdle { assertTrue(cleared.isEmpty()) }
        compose.onNodeWithText("清空服务器缓存").performClick()
        compose.onNodeWithText("清空").performClick()
        compose.runOnIdle { assertEquals(listOf(nas), cleared) }
        compose.onNodeWithText("0 个").assertExists()
        compose.onNodeWithText("3 个").assertExists()

        compose.onNodeWithText("清空本地缓存").performScrollTo().performClick()
        compose.onNodeWithText("清空本地缓存？").assertIsDisplayed()
        compose.onNodeWithText("清空").performClick()
        compose.runOnIdle { assertEquals(listOf(nas, local), cleared) }
        compose.onAllNodesWithText("0 个").assertCountEquals(2)
        compose.onNodeWithText("已生成 7 / 9", substring = true).assertExists()
    }
}
