package media.qimeng.next

import android.content.Context
import media.qimeng.next.runtime.LocalRuntime
import media.qimeng.next.runtime.RemoteOnlyRuntime

internal fun createRuntime(@Suppress("UNUSED_PARAMETER") context: Context): LocalRuntime = RemoteOnlyRuntime
