package media.qimeng.next.data

import android.content.ContextWrapper
import android.content.SharedPreferences
import androidx.test.platform.app.InstrumentationRegistry
import media.qimeng.next.model.Session
import media.qimeng.next.sdk.api.DefaultApi
import java.util.UUID

internal class CloudClient(val media: MediaRepository, val profiles: AuthorProfileRepository,
    val management: ManagementRepository, val provider: ApiProvider, val session: Session) {
    val api: DefaultApi = provider.api(session)
}

internal suspend fun signInToCloud(source: Boolean = false): CloudClient {
    val args = InstrumentationRegistry.getArguments()
    val address = requireNotNull(args.getString(if (source) "qimengSourceUrl" else "qimengServerUrl"))
    require(address == if (source) "http://127.0.0.1:18482/" else "http://127.0.0.1:18481/")
    val base = InstrumentationRegistry.getInstrumentation().targetContext
    val namespace = UUID.randomUUID().toString()
    val context = object : ContextWrapper(base) {
        override fun getSharedPreferences(name: String, mode: Int): SharedPreferences = base.getSharedPreferences("$namespace-$name", mode)
    }
    val settings = SecureSettings(context)
    val provider = ApiProvider(settings)
    val media = MediaRepository(settings, provider)
    media.login(address, requireNotNull(args.getString(if (source) "qimengSourceToken" else "qimengToken")))
    return CloudClient(media, AuthorProfileRepository(context, settings, provider), ManagementRepository(settings, provider),
        provider, requireNotNull(settings.session.value))
}
