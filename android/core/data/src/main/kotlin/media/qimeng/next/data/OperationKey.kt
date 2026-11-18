package media.qimeng.next.data

import java.security.MessageDigest

internal fun operationKey(prefix: String, vararg fields: String): String = prefix + "-" + MessageDigest.getInstance("SHA-256")
    .digest(fields.joinToString("\u0000").toByteArray(Charsets.UTF_8)).joinToString("") { "%02x".format(it) }
