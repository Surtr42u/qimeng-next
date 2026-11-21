package media.qimeng.next.embedded

import media.qimeng.next.runtime.RuntimePhase

/**
 * 前台 START / HEALTH_CHECK 是否应（重新）拉起 host（纯判断，单测锁定）。
 *
 * 服务里的 `running` 是被修复的旧状态，不作为存活依据（应用侧启动超时也会改写状态，
 * 旧 host 任务的 `stopSelf` 又会被后续 startId 覆盖）。真正的依据是 [hostLoopActive]
 * （任务已排队/正在运行）。[stopping] 且仍有 host 任务持有启动标号时不能在同一服务实例里
 * 重拉——任务可能卡在原生/Keystore/进程启动，解除阻塞后会与新任务各起一个子进程；
 * 但停止已经完成、没有任务持有时，新的显式 START（HEALTH_CHECK 已在 onStartCommand 提前
 * 返回）可以复活本实例。STOPPING 过渡期一律保持原位。
 */
internal fun shouldStartHost(
    phase: RuntimePhase?,
    stopping: Boolean,
    hostOwnerActive: Boolean,
    hostLoopActive: Boolean,
): Boolean = !hostLoopActive && phase != RuntimePhase.STOPPING &&
    (!stopping || !hostOwnerActive)
