package media.qimeng.next

import media.qimeng.next.model.ExtractionVocabulary

/**
 * 读取结果的合并规则（旧版 VocabularyEditViewModel 的「旧回读不得覆盖新草稿」口径）：
 * - 没有本地草稿或明确要求替换时，采用服务端生效词表；
 * - 有未保存草稿且服务端修订未变，只更新基准与重算状态，草稿保持；
 * - 有未保存草稿且服务端修订已变，标记冲突并保留草稿，等待用户确认后读取。
 */
internal fun ExtractionEditorState.afterRead(value: ExtractionVocabulary, replaceDraft: Boolean): ExtractionEditorState = when {
    replaceDraft || !dirty -> copy(base = value, draft = value.layer, loading = false, conflict = false)
    base?.revision == value.revision -> copy(base = value, loading = false)
    else -> copy(loading = false, conflict = true)
}
