package media.qimeng.next.embedded

/** Explicit synthetic instrumentation opt-in. This API and its buffer do not exist in release. */
object EmbeddedDebugDiagnostics {
    @Volatile private var enabled = false
    private val buffer = java.io.ByteArrayOutputStream()
    private var redactions = emptyList<String>()

    @Synchronized fun enable() { enabled = true; buffer.reset(); redactions = emptyList() }
    @Synchronized fun disable() { enabled = false; buffer.reset(); redactions = emptyList() }
    @Synchronized fun snapshot(): String = redactions.fold(buffer.toString("UTF-8")) { text, value -> text.replace(value, "<redacted>") }
    @Synchronized internal fun begin(values: List<String>) {
        if (!enabled) return
        buffer.reset()
        redactions = values.filter { it.isNotEmpty() }.sortedByDescending(String::length)
    }
    @Synchronized internal fun append(bytes: ByteArray, count: Int) {
        if (enabled && buffer.size() < 32_768) buffer.write(bytes, 0, minOf(count, 32_768 - buffer.size()))
    }
}

internal object HostDiagnostics {
    fun begin(values: List<String>) = EmbeddedDebugDiagnostics.begin(values)
    fun append(bytes: ByteArray, count: Int) = EmbeddedDebugDiagnostics.append(bytes, count)
}
