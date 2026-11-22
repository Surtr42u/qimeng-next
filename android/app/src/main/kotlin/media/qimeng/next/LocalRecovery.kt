package media.qimeng.next

import media.qimeng.next.runtime.RuntimePhase

/**
 * 连接失败横幅是否提供「连接本机影库」（纯判断，单测锁定）。
 *
 * 背景：本机运行模块可用时，失败横幅只有「重试」会留下两条死路：
 * - 保存的会话仍指向一台已不可达的远端服务器，自动恢复只认本机端点，永远不会启动本机服务；
 * - 会话已经指向本机端点，但运行层没有进入 READY（启动失败、子进程退出或启动卡住），
 *   数据层只能报通用的「连接失败」，运行层自己的失败原因到不了首页。
 *
 * 判定只决定「要不要给出口」，不区分具体地址：
 * - 当前会话不是本机端点（按 [media.qimeng.next.model.Session] 地址比较，或调用方用
 *   `LocalRuntime.ownedEndpoints` 确认过归属）：只有真正的传输层失败才提供，401/403/409/5xx
 *   等已收到 HTTP 应答的业务错误不能用切换媒体库来掩盖；
 * - 当前会话就是本机端点：传输失败或运行层未 READY 都提供。运行层 READY 时的业务错误仍不提供。
 *
 * 判定通过也只增加一个需要点按的动作；启动本机服务、登录与队列交接都走既有的本机连接
 * 流程，不在失败时自动切换。
 */
internal fun shouldOfferLocalRecovery(
    runtimeAvailable: Boolean,
    currentBaseUrl: String?,
    runtimeBaseUrl: String?,
    failureIsTransport: Boolean,
    runtimePhase: RuntimePhase?,
    currentIsOwnedLocalEndpoint: Boolean,
): Boolean {
    if (!runtimeAvailable) return false
    val current = currentBaseUrl?.takeIf(String::isNotBlank) ?: return false
    val local = runtimeBaseUrl?.takeIf(String::isNotBlank) ?: return false
    val currentIsLocal = currentIsOwnedLocalEndpoint || sameEndpoint(current, local)
    return if (currentIsLocal) failureIsTransport || runtimePhase != RuntimePhase.READY
    else failureIsTransport
}

/**
 * 两个根地址是否指向同一个入口：只忽略结尾的 `/`（保存的会话地址经 HttpUrl 规范化后带
 * 尾斜杠，本机运行层的配置值可能不带）。scheme 与端口保持参与比较——旧版
 * `https://127.0.0.1:port/` 与本机当前的 loopback HTTP 入口不同，仍应给出显式恢复出口。
 */
internal fun sameEndpoint(left: String?, right: String?): Boolean {
    val a = left?.takeIf(String::isNotBlank) ?: return false
    val b = right?.takeIf(String::isNotBlank) ?: return false
    return a.trim().trimEnd('/') == b.trim().trimEnd('/')
}

/**
 * 失败横幅的「重试」是否应改走本机连接流程（启动/重启运行层并等待 READY）。
 *
 * 本机端点上的普通 `browse()` 重试只会再次请求一个没有监听的 loopback 端口，永远不可能
 * 成功；运行层未 READY 时必须先把它拉起来。READY 时保持原有列表重试，避免每次网络抖动
 * 都去动本机进程。
 */
internal fun shouldRestartLocalRuntime(
    runtimeAvailable: Boolean,
    sessionIsLocalEndpoint: Boolean,
    runtimePhase: RuntimePhase?,
): Boolean = runtimeAvailable && sessionIsLocalEndpoint &&
    (runtimePhase == RuntimePhase.FAILED || runtimePhase == RuntimePhase.STARTING || runtimePhase == RuntimePhase.STOPPED)

/**
 * 失败横幅是否应优先显示运行层自己的状态/失败原因（启动中、子进程退出码、凭证缺失等）。
 * 数据层对 loopback 断连只能给出通用 IOException 文案，运行层的消息才有可操作信息。
 */
internal fun shouldPreferRuntimeFailureMessage(
    runtimeAvailable: Boolean,
    sessionIsLocalEndpoint: Boolean,
    runtimePhase: RuntimePhase?,
    runtimeMessage: String?,
): Boolean = runtimeAvailable && sessionIsLocalEndpoint && !runtimeMessage.isNullOrBlank() &&
    (runtimePhase == RuntimePhase.FAILED || runtimePhase == RuntimePhase.STARTING || runtimePhase == RuntimePhase.STOPPED)

/**
 * 连接失败横幅是否补充「重建本机索引」（纯判断，单测锁定）。
 *
 * 只有本机端点、运行层 FAILED 且失败文案带子进程退出码时才提供：那说明控制目录里的索引
 * 数据（SQLite 所有权锁、损坏的目录库、未完成恢复标记）让新进程启动即退出，单纯重启
 * 运行层不会自愈。启动超时、凭据缺失、远端会话或运行层未失败都不提供，避免把可恢复的
 * 配置问题当成数据损坏。
 */
internal fun shouldOfferLocalIndexRebuild(
    runtimeAvailable: Boolean,
    sessionIsLocalEndpoint: Boolean,
    runtimePhase: RuntimePhase?,
    runtimeMessage: String?,
): Boolean = runtimeAvailable && sessionIsLocalEndpoint && runtimePhase == RuntimePhase.FAILED &&
    runtimeMessage?.contains(EXIT_CODE_MARKER) == true

/** 运行层子进程退出码文案的标记；具体退出码不参与判定。 */
private const val EXIT_CODE_MARKER = "退出码"
