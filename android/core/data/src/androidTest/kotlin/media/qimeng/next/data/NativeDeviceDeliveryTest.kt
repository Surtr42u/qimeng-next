package media.qimeng.next.data

import android.util.Base64
import androidx.test.platform.app.InstrumentationRegistry
import kotlinx.coroutines.runBlocking
import media.qimeng.next.sdk.model.*
import org.junit.Assert.*
import org.junit.Test
import java.io.File
import java.security.SecureRandom
import java.util.UUID

class NativeDeviceDeliveryTest {
    @Test fun twoNativeInstancesResumeAndReplayOnePackageAndProtectLocalEdits() = runBlocking {
        val source = signInToCloud(source = true)
        val target = signInToCloud()
        val sourceId = requireNotNull(source.api.getInstance().bodyOrThrow().id)
        val targetId = requireNotNull(target.api.getInstance().bodyOrThrow().id)
        assertNotEquals(sourceId, targetId)
        val device = "native-cloud-phone"
        val secret = ByteArray(32).also { SecureRandom().nextBytes(it) }.let { Base64.encodeToString(it, Base64.URL_SAFE or Base64.NO_WRAP or Base64.NO_PADDING) }
        source.api.createDeviceCredential("native-cloud-registration", DeviceCredentialInput(deviceId = device, label = "合成接收设备", credentialSecret = secret)).bodyOrThrow()
        val peer = source.provider.scoped(source.session.baseUrl, null, secret)
        assertEquals(device, peer.getCurrentSession().bodyOrThrow().deviceId)
        val root = File(InstrumentationRegistry.getInstrumentation().targetContext.cacheDir, "native-transfer-${UUID.randomUUID()}")
        check(root.mkdir())
        val author = source.media.createAuthor("云端原生交付作者")
        val status = source.api.getAuthor(author.id).bodyOrThrow().fieldStatus
        assertEquals("confirmed", status["identity"]?.value)
        assertEquals("missing", status["biography"]?.value)
        suspend fun changeName(client: CloudClient, id: String, name: String): Long {
            val doc = client.profiles.profile(id)
            val block = doc.blocks.single { it.block == "basic" }
            return client.profiles.save(doc, block, block.values.copy(publicName = name), block.evidence, null, false).revision
        }
        fun selection(revision: Long) = NativeDeliverySelection(kind = NativeDeliveryKind.authorProfileBlock, sourceObjectId = author.id,
            expectedRevision = revision, block = NativeDeliveryBlock.basic)
        suspend fun deliver(tag: String, items: List<NativeDeliverySelection>, replace: Boolean): NativeDeliveryPlan {
            val task = source.api.publishNativeDelivery("publish-$tag", NativeDeliveryPublish(device, targetId, items)).bodyOrThrow()
            assertTrue(peer.listDeviceApplyTasks(device, state = DeviceApplyState.pending, protocolVersion = 2, limit = 32).bodyOrThrow().items.any { it.id == task.id })
            val claimed = peer.claimDeviceApplyTask(task.id, "claim-$tag", RevisionRequest(task.revision)).bodyOrThrow()
            val binding = DeviceSegments.nativeBinding(claimed, sourceId, targetId)
            assertThrows(IllegalStateException::class.java) { DeviceSegments.nativeBinding(claimed, sourceId, sourceId) }
            val segment = claimed.segments.single()
            if (tag == "first") {
                val partial = peer.getDeviceApplySegment(task.id, segment.kind, "bytes=0-30", "\"${segment.sha256}\"")
                assertEquals(206, partial.code())
                val prefix = partial.bodyOrThrow().use { it.bytes() }
                assertEquals(31, prefix.size)
                File(root, operationKey("segment", sourceId, task.id, segment.kind.value)).writeBytes(prefix)
            }
            val file = DeviceSegments.fetch(root, sourceId, claimed, segment, peer)
            try {
                val applied = DeviceSegments.applyNative(target.api, file, binding, replace)
                assertEquals("Read the same local result after a lost commit response", applied, DeviceSegments.applyNative(target.api, file, binding, replace))
                val receipt = target.api.getNativeDelivery(requireNotNull(applied.receipt)).bodyOrThrow()
                assertTrue(receipt.items.all { !it.receiptId.isNullOrBlank() })
                val report = DeviceApplyReport(expectedRevision = claimed.revision,
                    state = DeviceApplyReport.State.entries.single { it.value == applied.state }, results = listOf(applied.toOutcome()))
                peer.reportDeviceApplyTask(task.id, "report-$tag", report).bodyOrThrow()
                peer.reportDeviceApplyTask(task.id, "report-$tag", report).bodyOrThrow()
                assertEquals(report.results, source.api.getDeviceApplyTask(task.id).bodyOrThrow().results)
                return receipt
            } finally { file.delete() }
        }
        try {
            val firstRevision = changeName(source, author.id, "初次选定的资料")
            val text = "1  原生 TXT 样本\n作品\nselected.png\n"
            val list = requireNotNull(source.api.importAuthorList("source-native-txt", ImportAuthorListRequest("原生交付片段.txt", text, 0L)).bodyOrThrow().list)
            val originalSelection = listOf(selection(firstRevision), NativeDeliverySelection(NativeDeliveryKind.authorList, list.id, list.revision))
            val initial = deliver("first", originalSelection, false)
            assertEquals(NativeDeliveryPlanState.applied, initial.state)
            val localAuthor = requireNotNull(initial.items.single { it.kind == NativeDeliveryKind.authorProfileBlock }.targetObjectId)
            val localList = requireNotNull(initial.items.single { it.kind == NativeDeliveryKind.authorList }.targetObjectId)
            assertEquals(list.name, target.api.getAuthorList(localList).bodyOrThrow().name)
            assertEquals(source.api.exportAuthorList(list.id).bodyOrThrow().use { it.string() }, target.api.exportAuthorList(localList).bodyOrThrow().use { it.string() })
            suspend fun actualName() = target.profiles.profile(localAuthor).blocks.single { it.block == "basic" }.values.publicName
            assertEquals("初次选定的资料", actualName())
            val replay = deliver("same-package-another-task", originalSelection, true)
            assertEquals(initial.id, replay.id)
            assertEquals(initial.items, replay.items)

            val keep = deliver("keep", listOf(selection(changeName(source, author.id, "保留时的来源更新"))), false)
            assertEquals("keep_existing", keep.items.single().code)
            assertEquals("初次选定的资料", actualName())
            val replaced = deliver("replace", listOf(selection(changeName(source, author.id, "新来源值"))), true)
            assertEquals(NativeDeliveryItemState.applied, replaced.items.single().state)
            assertEquals("新来源值", actualName())
            changeName(target, localAuthor, "本机人工校订")
            val protected = deliver("protect", listOf(selection(changeName(source, author.id, "后续来源值"))), true)
            assertEquals(NativeDeliveryPlanState.partial, protected.state)
            assertEquals("human_protected", protected.items.single().code)
            assertEquals("本机人工校订", actualName())
        } finally {
            root.delete()
            source.media.logout()
            target.media.logout()
        }
    }
}
