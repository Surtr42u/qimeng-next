package media.qimeng.next

import media.qimeng.next.data.ApiFailure
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.IOException
import java.net.SocketTimeoutException

/**
 * 详情数据刷新失败的提示策略（旧版口径：种子在屏上时保持画面、静默补数据失败）。
 *
 * 只有「传输失败 + 屏上已有当前资产」才静默；无内容可显示、或 HTTP 业务失败
 * （404/权限/冲突）仍然要给用户错误出口。
 */
class DetailFailurePolicyTest {

    private val transport = IOException("connection reset by peer")
    private val timeout = SocketTimeoutException("read timed out")

    @Test fun atransportFailureKeepsTheSeedSilent() {
        assertTrue(detailFailureIsSilent(transport, assetDisplayed = true))
        assertTrue(detailFailureIsSilent(timeout, assetDisplayed = true))
    }

    @Test fun aTransportFailureWithoutContentStillSurfaces() {
        assertFalse(detailFailureIsSilent(transport, assetDisplayed = false))
        assertFalse(detailFailureIsSilent(timeout, assetDisplayed = false))
    }

    @Test fun httpAnswersAreNeverSilenced() {
        assertFalse(detailFailureIsSilent(ApiFailure(404, "对象不存在或已被移除"), assetDisplayed = true))
        assertFalse(detailFailureIsSilent(ApiFailure(401, "登录已失效，请重新登录"), assetDisplayed = true))
        assertFalse(detailFailureIsSilent(ApiFailure(409, "服务器状态已变化或存在重复，请刷新核对后再决定"), assetDisplayed = true))
        assertFalse(detailFailureIsSilent(ApiFailure(500, "服务器暂时无法完成请求"), assetDisplayed = true))
    }

    @Test fun decodingAndValidationFailuresAreNeverSilenced() {
        assertFalse(detailFailureIsSilent(IllegalStateException("请先连接此媒体原来的服务器"), assetDisplayed = true))
        assertFalse(detailFailureIsSilent(IllegalArgumentException("媒体地址无效"), assetDisplayed = true))
        assertFalse(detailFailureIsSilent(RuntimeException("操作未完成"), assetDisplayed = true))
    }
}
