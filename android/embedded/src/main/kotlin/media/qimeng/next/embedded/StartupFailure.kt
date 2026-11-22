package media.qimeng.next.embedded

/** 启动失败详情上限：Go 子进程已截断到 400 字符，宿主再按单行与长度兜底。 */
private const val FAILURE_DETAIL_LIMIT = 400

/**
 * 启动失败横幅文案（纯函数，单测锁定）。release 构建丢弃子进程输出，控制目录旁的
 * `<回执>.failed` 是唯一能说明数据库损坏、目录被占用或恢复未完成等原因的通道。
 * 退出码总是保留；详情压成单行并截断，空详情退回原文案。
 */
internal fun startupFailureMessage(exitCode: Int, detail: String?): String {
    val base = "本机程序启动失败（退出码 $exitCode）"
    val text = detail?.replace(Regex("\\s+"), " ")?.trim()?.take(FAILURE_DETAIL_LIMIT).orEmpty()
    return if (text.isEmpty()) base else "$base：$text"
}
