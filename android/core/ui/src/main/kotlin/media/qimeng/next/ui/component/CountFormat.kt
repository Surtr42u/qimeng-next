package media.qimeng.next.ui.component

import java.math.BigDecimal
import java.math.RoundingMode
import java.util.Locale

/** Existing QimengFormat.formatCount presentation, widened to the new API's Long count. */
fun formatCount(n: Long?): String {
    if (n == null) return "0"
    if (n >= 10_000) {
        val w = n / 10_000.0
        val s = if (w >= 100.0) BigDecimal(w).setScale(0, RoundingMode.HALF_UP).toPlainString()
            else String.format(Locale.US, "%.1f", w).removeSuffix(".0")
        return "${s}万"
    }
    return String.format(Locale.US, "%,d", n)
}
