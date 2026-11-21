package media.qimeng.next.screen

import java.io.IOException
import java.net.SocketTimeoutException
import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * 详情原图失败归因锁定（旧版 ImageLoadErrorClassifier 口径）：传输类 →「网络不畅」，
 * 真解码失败 →「该文件无法解码」；网络抖动不得报成文件损坏。
 */
class ImageFailureClassifierTest {

    @Test fun ioFailuresReadAsNetwork() {
        assertEquals(ImageFailureKind.NETWORK, classifyImageFailure(SocketTimeoutException("timeout")))
        assertEquals(ImageFailureKind.NETWORK, classifyImageFailure(IllegalStateException("wrapped", IOException("reset"))))
    }

    @Test fun decodeFailuresReadAsUndecodable() {
        assertEquals(ImageFailureKind.DECODE, classifyImageFailure(IllegalArgumentException("unsupported format")))
        assertEquals(ImageFailureKind.DECODE, classifyImageFailure(RuntimeException("no cause")))
    }

    @Test fun causeChainDepthIsBounded() {
        var failure: Throwable = IOException("too deep to matter")
        repeat(MAX_CAUSE_CHAIN_DEPTH) { failure = RuntimeException("wrap", failure) }
        assertEquals(ImageFailureKind.DECODE, classifyImageFailure(failure))
    }

    @Test fun copyMatchesTheOldVersion() {
        assertEquals("网络不畅，图片加载失败，请重试", imageFailureMessage(ImageFailureKind.NETWORK))
        assertEquals("该文件无法解码，可能已损坏或格式不受支持", imageFailureMessage(ImageFailureKind.DECODE))
    }
}
