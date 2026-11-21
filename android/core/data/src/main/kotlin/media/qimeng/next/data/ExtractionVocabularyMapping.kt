package media.qimeng.next.data

import media.qimeng.next.model.ExtractionGroup
import media.qimeng.next.model.ExtractionLayer
import media.qimeng.next.model.ExtractionRole
import media.qimeng.next.model.ExtractionVocabulary
import media.qimeng.next.sdk.model.SourceRuleCharacter
import media.qimeng.next.sdk.model.SourceRuleGroup
import media.qimeng.next.sdk.model.SourceRuleSettings
import media.qimeng.next.sdk.model.SourceRuleUpdate

/**
 * 原名提取词表的域模型 ↔ SDK 生成物映射（旧版 VocabularyEditMappers 的 Next 对应物）。
 *
 * 协议 GET /sources/custom-groups 返回整层生效词表与修订/重算状态，PUT 用 expectedRevision
 * 整体替换整层：提交的 groups 就是生效名单，空数组=清空，不是「不提交=保留」；stopWords
 * 同样恒为显式数组（空数组=清空回内置基线）。因此保存载荷不允许省略任何子列表，空列表
 * 必须原样序列化——这条语义由本文件的单测锁定，UI 只编辑 [ExtractionLayer]。
 */
internal fun SourceRuleSettings.toExtractionVocabulary(): ExtractionVocabulary = ExtractionVocabulary(
    layer = ExtractionLayer(
        groups = groups.map { group ->
            ExtractionGroup(
                canonical = group.canonical,
                variants = group.variants,
                roles = group.characters.map { role -> ExtractionRole(role.canonical, role.aliases) },
            )
        },
        stopWords = stopWords,
    ),
    revision = revision,
    rebuildState = rebuildState.value,
    processed = processed,
    matched = matched,
    ambiguous = ambiguous,
    errorCode = errorCode,
)

/** 端内编辑层 → 整体替换载荷；revision 是读取时看到的服务端修订，服务端据此拒绝过期写入。 */
internal fun ExtractionLayer.toSourceRuleUpdate(revision: Long) = SourceRuleUpdate(
    groups = groups.map { group ->
        SourceRuleGroup(
            canonical = group.canonical,
            variants = group.variants,
            characters = group.roles.map { role -> SourceRuleCharacter(canonical = role.canonical, aliases = role.aliases) },
        )
    },
    stopWords = stopWords,
    expectedRevision = revision,
)
