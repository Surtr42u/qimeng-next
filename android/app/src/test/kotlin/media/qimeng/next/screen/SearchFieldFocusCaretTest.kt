package media.qimeng.next.screen

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

/**
 * 搜索框重新聚焦的光标落点：只在「文本非空 + 选择折叠在 0」时移到文本末尾，
 * 让结果态提交后点回搜索栏的退格能直接删掉上一轮问句；其余选择一律不动。
 */
class SearchFieldFocusCaretTest {

    @Test
    fun `文本非空且选择折叠在左端时移到末尾`() {
        assertEquals(4, searchFieldFocusCaret(textLength = 4, selectionStart = 0, selectionEnd = 0))
    }

    @Test
    fun `空文本保持选择不移动`() {
        assertNull(searchFieldFocusCaret(textLength = 0, selectionStart = 0, selectionEnd = 0))
    }

    @Test
    fun `光标已在末尾时不重复移动`() {
        assertNull(searchFieldFocusCaret(textLength = 4, selectionStart = 4, selectionEnd = 4))
    }

    @Test
    fun `点选中间位置或框选文本时不移动`() {
        assertNull(searchFieldFocusCaret(textLength = 4, selectionStart = 2, selectionEnd = 2))
        assertNull(searchFieldFocusCaret(textLength = 4, selectionStart = 1, selectionEnd = 3))
        assertNull(searchFieldFocusCaret(textLength = 4, selectionStart = 0, selectionEnd = 4))
    }
}
