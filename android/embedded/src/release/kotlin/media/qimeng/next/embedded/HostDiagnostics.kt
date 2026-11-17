package media.qimeng.next.embedded

/** Release builds never retain native output, including private paths and media names. */
internal object HostDiagnostics {
    fun begin(@Suppress("UNUSED_PARAMETER") values: List<String>) = Unit
    fun append(@Suppress("UNUSED_PARAMETER") bytes: ByteArray, @Suppress("UNUSED_PARAMETER") count: Int) = Unit
}
