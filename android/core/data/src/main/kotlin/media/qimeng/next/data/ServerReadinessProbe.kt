package media.qimeng.next.data

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Deferred
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.async
import kotlinx.coroutines.delay
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withTimeoutOrNull
import media.qimeng.next.model.Session
import media.qimeng.next.model.sameTarget
import java.io.IOException

/**
 * 服务端就绪探针（旧版 core/network/ServerReadinessProbe 的等价物，见 docs/status 对照）。
 *
 * 为什么存在：无论是内嵌本机服务还是 PC/NAS 单机服务，冷启动/重启都有秒级的「未就绪
 * 窗口」——进程起来 → SQLite 迁移 → ffmpeg 自检 → 才开始监听。此前客户端在这段时间
 * 里发出的首个业务请求必然 IOException，直接被 UI 层的失败映射变成「连接失败」横幅。
 * 探针在业务请求之前以 300ms 粒度轮询就绪端点，把等待前移；预算
 * 耗尽返回 false，调用方仍走既有失败/重试路径，不改变任何错误语义。
 *
 * 端点：本 OpenAPI 没有公开 /healthz；`GET /api/v1/instance` 免鉴权（登录前的实例探测
 * 也用同一端点），所以探针走 anonymous 客户端，不需要也不应携带业务凭据。
 *
 * 并发语义：多调用方并发 [awaitReady] 单飞共享同一次探测；就绪结论在
 * [ServerReadinessPolicy.READY_FRESH_MS] 内直接复用，不重复打探针。单飞与新鲜度都按
 * 目标连接（baseUrl + instanceId）分键：5s 内从服务器 A 切到 B 时 B 必须自己探一次，
 * 不能拿 A 的结论冒充 B 就绪；A 的在途探测也不会被 B 复用。
 */
internal class ServerReadinessProbe(
    private val settings: SecureSettings,
    private val provider: ApiProvider,
    private val scope: CoroutineScope = CoroutineScope(SupervisorJob() + Dispatchers.IO),
    private val nowElapsedMs: () -> Long = { System.nanoTime() / 1_000_000L },
) {
    private val mutex = Mutex()

    /** 一次共享探测连同它的目标键；null = 当前没有探测在跑。 */
    private data class Flight(val targetKey: String, val result: Deferred<Boolean>)

    /** 进行中的共享探测。 */
    private var inFlight: Flight? = null

    /** 「哪个连接、在什么时刻」被判定就绪；键与时间戳一起替换，读取不会拆成两次。 */
    private data class ReadyMark(val targetKey: String, val elapsedMs: Long)

    @Volatile
    private var lastReady: ReadyMark? = null

    /**
     * 等待 [target] 的服务端就绪。
     *
     * @return true = 就绪端点已通；false = 目标已不是当前会话，或预算内始终未就绪。
     *   调用方应照常发起业务请求，由既有失败路径给出用户可见反馈。
     */
    suspend fun awaitReady(target: Session, budgetMs: Long = ServerReadinessPolicy.BUDGET_MS): Boolean {
        // 目标已不是当前选中的连接时不该再为它等待：用户可能刚切了服务器或登出。
        if (!sameTarget(target, settings.session.value)) return false
        val key = readinessKey(target)
        val mark = lastReady
        if (ServerReadinessPolicy.isReadyFresh(mark?.targetKey, key, mark?.elapsedMs, nowElapsedMs())) return true
        val deferred = mutex.withLock {
            val running = inFlight
            if (running != null && ServerReadinessPolicy.reuseInFlight(running.targetKey, key, running.result.isActive)) running.result
            else scope.async { pollUntilReady(target, key, budgetMs) }.also { inFlight = Flight(key, it) }
        }
        val ready = try {
            deferred.await()
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (_: Exception) {
            // pollUntilReady 按设计不抛；兜底保证「探测异常 = 未就绪」而不是崩调用方。
            false
        } finally {
            mutex.withLock { if (inFlight?.result === deferred && deferred.isCompleted) inFlight = null }
        }
        return ready && sameTarget(target, settings.session.value)
    }

    private suspend fun pollUntilReady(target: Session, targetKey: String, budgetMs: Long): Boolean {
        val api = try {
            // 探测走 anonymous 客户端（无认证头、无认证客户端的 300/900ms 重试拦截器），
            // 证书取当前目标的：轮询本身要保持 300ms 的节奏。中途换连接是显式登出流，
            // 结果会在 awaitReady 末尾按 sameTarget 丢弃。
            provider.anonymous(target.baseUrl, settings.certificateFor(target))
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (_: Exception) {
            return false
        }
        val deadline = nowElapsedMs() + budgetMs
        while (ServerReadinessPolicy.shouldKeepPolling(nowElapsedMs(), deadline)) {
            val remainingMs = deadline - nowElapsedMs()
            try {
                // 单次探测不超过预算剩余：不可达地址的连接超时（15s）不能把「10s 预算」拖成 25s。
                val instance = withTimeoutOrNull(remainingMs) { api.getInstance().bodyOrThrow() }
                if (instance != null) {
                    lastReady = ReadyMark(targetKey, nowElapsedMs())
                    return true
                }
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (failure: ApiFailure) {
                // 端点免鉴权且恒 200：4xx 说明对端不是绮梦服务端，再等也不会「就绪」，
                // 提前终止不白耗预算。5xx 是地址对但服务端自身故障，按未就绪继续等。
                if (ServerReadinessPolicy.isTerminalProbeStatus(failure.status)) return false
            } catch (_: IOException) {
                // 连接拒绝/不可达 = 还没监听起来，等待窗口内的预期状态。
            } catch (_: Exception) {
                // 应答无法解析等异常也按未就绪处理：调用方照常走业务请求的失败路径。
            }
            delay(ServerReadinessPolicy.POLL_INTERVAL_MS)
        }
        return false
    }
}

/** 就绪结论的分键：地址与实例都要相同才算同一台服务端（换号/换地址都不复用）。 */
internal fun readinessKey(target: Session): String = "${target.baseUrl}|${target.instanceId}"

/**
 * 就绪探针的纯判定（单测锁定）：
 * - 就绪结论只在 [READY_FRESH_MS] 短窗内复用（严格小于；窗口外重新探测）；
 * - 新鲜度按目标连接分键：A 的就绪结论不能用来回答 B；
 * - 预算按 elapsed 时钟判定，到点即放弃，不无限等待；
 * - 进行中的探测只有「同一目标且仍然 active」才能被共享；
 * - 4xx 是「对端不是绮梦服务端」的终态，5xx/传输失败仍按未就绪继续轮询。
 */
internal object ServerReadinessPolicy {
    /** 轮询间隔：内嵌服务端秒级就绪，300ms 粒度贴合窗口又不刷爆回环。 */
    const val POLL_INTERVAL_MS = 300L

    /** 默认等待预算：覆盖最慢的冷启动（低端机首启 ffmpeg 自检偏慢）。 */
    const val BUDGET_MS = 10_000L

    /** 就绪结论短窗：窗口内的重复调用直接复用，避免多调用方重复打探针。 */
    const val READY_FRESH_MS = 5_000L

    fun isReadyFresh(
        lastReadyTargetKey: String?,
        requestedTargetKey: String,
        lastReadyElapsedMs: Long?,
        nowElapsedMs: Long,
        freshnessMs: Long = READY_FRESH_MS,
    ): Boolean {
        if (lastReadyTargetKey != requestedTargetKey) return false
        val last = lastReadyElapsedMs ?: return false
        return nowElapsedMs - last in 0 until freshnessMs
    }

    fun shouldKeepPolling(nowElapsedMs: Long, deadlineElapsedMs: Long): Boolean = nowElapsedMs < deadlineElapsedMs

    fun reuseInFlight(inFlightTargetKey: String, requestedTargetKey: String, inFlightActive: Boolean): Boolean =
        inFlightActive && inFlightTargetKey == requestedTargetKey

    fun isTerminalProbeStatus(status: Int): Boolean = status in 400..499
}
