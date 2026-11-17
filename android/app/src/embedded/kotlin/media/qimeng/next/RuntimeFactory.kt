package media.qimeng.next

import android.content.Context
import media.qimeng.next.embedded.EmbeddedRuntime
import media.qimeng.next.runtime.LocalRuntime

internal fun createRuntime(context: Context): LocalRuntime = EmbeddedRuntime(context)
