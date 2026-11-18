package media.qimeng.next.data

import androidx.test.platform.app.InstrumentationRegistry
import kotlinx.coroutines.runBlocking
import media.qimeng.next.sdk.model.DeviceApplySegment
import media.qimeng.next.sdk.model.DeviceSegmentKind
import org.json.JSONArray
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test
import java.io.File
import java.security.MessageDigest

class DeviceProfileDeliveryTest {
    @Test fun claimedV2SegmentKeepsOrReplacesImportedValuesAndReportsHumanProtection() = runBlocking {
        val args = InstrumentationRegistry.getArguments()
        val address = requireNotNull(args.getString("qimengServerUrl"))
        require(address == "http://127.0.0.1:18481/")
        val context = InstrumentationRegistry.getInstrumentation().targetContext
        val settings = SecureSettings(context)
        val provider = ApiProvider(settings)
        val media = MediaRepository(settings, provider)
        val profiles = AuthorProfileRepository(context, settings, provider)
        media.login(address, requireNotNull(args.getString("qimengToken")))
        val api = provider.api(requireNotNull(settings.session.value))
        val author = media.createAuthor("云端设备资料样本")
        val file = File.createTempFile("synthetic-device-v2-", ".json", context.cacheDir)
        try {
            suspend fun apply(nickname: String, key: String, replace: Boolean): ReceivedSegment {
                val profile = JSONObject().put("authorId", author.id).put("status", "verified").put("nickname", nickname)
                val entry = JSONObject().put("authorId", author.id).put("displayName", author.name).put("profileJson", profile.toString())
                val data = JSONObject().put("authors", JSONArray().put(entry)).put("mediaFiles", JSONArray()).put("viewEvents", JSONArray())
                val bytes = JSONObject().put("format", "qimeng_backup").put("schemaVersion", 2).put("appIdentifier", "synthetic.cloud")
                    .put("data", data).toString().toByteArray(Charsets.UTF_8)
                file.writeBytes(bytes)
                val hash = MessageDigest.getInstance("SHA-256").digest(bytes).joinToString("") { "%02x".format(it) }
                val segment = DeviceApplySegment(kind = DeviceSegmentKind.v2Backup, bytes = bytes.size.toLong(), sha256 = hash, sourceId = "synthetic-device-import")
                return DeviceSegments.apply(api, file, segment, key, replace)
            }

            suspend fun basic() = profiles.profile(author.id).blocks.single { it.block == "basic" }
            assertEquals("applied", apply("初次导入", "device-first", false).state)
            assertEquals("初次导入", basic().values.publicName)
            val kept = apply("更新导入", "device-updated", false)
            assertEquals("applied", kept.state)
            assertEquals("初次导入", basic().values.publicName)
            val replaced = apply("更新导入", "device-updated", true)
            assertEquals("applied", replaced.state)
            assertNotEquals(kept.receipt, replaced.receipt)
            assertEquals("更新导入", basic().values.publicName)
            assertEquals(replaced, apply("更新导入", "device-updated", true))

            val document = profiles.profile(author.id)
            val original = document.blocks.single { it.block == "basic" }
            profiles.save(document, original, original.values.copy(publicName = "人工校订"), original.evidence, null, false)
            val protected = apply("另一个导入值", "device-protected", true)
            assertEquals("partial", protected.state)
            assertEquals("v2_profiles_protected", protected.code)
            assertEquals(64, requireNotNull(protected.hash).length)
            val receipt = api.getV2MigrationPlan(requireNotNull(protected.receipt)).bodyOrThrow()
            assertNotNull(receipt.checkpointId)
            assertEquals(1L, receipt.counts["profileBlocksProtected"])
            assertEquals("人工校订", basic().values.publicName)
        } finally {
            file.delete()
            media.logout()
        }
    }
}
