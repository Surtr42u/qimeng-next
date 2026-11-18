package media.qimeng.next.data

import android.os.ParcelFileDescriptor
import android.util.Base64
import androidx.test.platform.app.InstrumentationRegistry
import kotlinx.coroutines.runBlocking
import media.qimeng.next.model.Asset
import media.qimeng.next.model.DevicePackageBinding
import media.qimeng.next.sdk.api.DefaultApi
import media.qimeng.next.sdk.model.*
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.RequestBody.Companion.asRequestBody
import okhttp3.RequestBody.Companion.toRequestBody
import org.junit.After
import org.junit.Assert.*
import org.junit.Before
import org.junit.Test
import java.io.File
import java.nio.ByteBuffer
import java.security.MessageDigest
import java.security.SecureRandom
import java.util.UUID

class NativeDeviceDeliveryTest {
    private val device = "native-cloud-${UUID.randomUUID()}"
    private lateinit var source: CloudClient
    private lateinit var target: CloudClient
    private lateinit var sourceId: String
    private lateinit var targetId: String
    private lateinit var peer: DefaultApi
    private lateinit var root: File

    @Before fun connectIndependentInstances() = runBlocking {
        source = signInToCloud(source = true)
        target = signInToCloud()
        sourceId = source.api.getInstance().bodyOrThrow().id
        targetId = target.api.getInstance().bodyOrThrow().id
        assertNotEquals(sourceId, targetId)
        val secret = ByteArray(32).also { SecureRandom().nextBytes(it) }.let { Base64.encodeToString(it, Base64.URL_SAFE or Base64.NO_WRAP or Base64.NO_PADDING) }
        source.api.createDeviceCredential("register-$device", DeviceCredentialInput(deviceId = device, label = "合成接收设备", credentialSecret = secret)).bodyOrThrow()
        peer = source.provider.scoped(source.session.baseUrl, null, secret)
        assertEquals(device, peer.getCurrentSession().bodyOrThrow().deviceId)
        root = File(InstrumentationRegistry.getInstrumentation().targetContext.cacheDir, device)
        check(root.mkdir())
    }

    @After fun disconnect(): Unit = runBlocking {
        if (::root.isInitialized) root.delete()
        if (::source.isInitialized) source.media.logout()
        if (::target.isInitialized) target.media.logout()
    }

    @Test fun twoNativeInstancesResumeAndReplayOnePackageAndProtectLocalEdits() = runBlocking {
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
    }

    @Test fun timelineMarkStaysOnItsExplicitAssetAndContentVersion() = runBlocking {
        val instrument = InstrumentationRegistry.getInstrumentation()
        val fixture = ParcelFileDescriptor.AutoCloseInputStream(instrument.uiAutomation.executeShellCommand("cat /data/local/tmp/qimeng-next-test.mp4")).use { it.readBytes() }
        check(fixture.size in 100..8_000_000)
        // A valid ISO BMFF free box gives this synthetic video its own digest, independent of the gallery fixture.
        fun videoBytes(version: Int): ByteArray = fixture + ByteBuffer.allocate(12).putInt(12).put("free".toByteArray(Charsets.US_ASCII)).putInt(version).array()
        val bytes = videoBytes(1)
        val hash = MessageDigest.getInstance("SHA-256").digest(bytes).joinToString("") { "%02x".format(it) }
        val sourceLibrary = source.media.createLibrary("云端标记来源", false)
        val targetLibrary = target.media.createLibrary("云端标记目标", false)
        val original = uploadVideo(source, sourceLibrary.id, "source-mark.mp4", bytes)
        try {
            val chosen = uploadVideo(target, targetLibrary.id, "chosen-mark.mp4", bytes)
            try {
                assertNotEquals(original.id, chosen.id)
                suspend fun makeMark(label: String) = source.api.createTimelineMark(original.id, "$device-mark-$label",
                    TimelineInput(positionMs = 350L, label = label, contentVersion = original.version, color = "#336699")).bodyOrThrow()
                fun select(mark: TimelineMark, assetId: String = chosen.id) = NativeDeliverySelection(kind = NativeDeliveryKind.timelineMark,
                    sourceObjectId = mark.id, expectedRevision = mark.revision, sourceAssetId = original.id, sourceContentVersion = original.version,
                    targetAssetId = assetId, targetContentVersion = chosen.version)
                suspend fun marks() = target.api.listTimelineMarks(chosen.id).bodyOrThrow().items

                val first = makeMark("opening")
                val applied = deliver("mark-first", listOf(select(first)))
                assertEquals(NativeDeliveryPlanState.applied, applied.state)
                val item = applied.items.single()
                assertEquals(NativeDeliveryItemState.applied, item.state)
                assertEquals(first.id, item.sourceObjectId)
                assertEquals(original.id, item.sourceAssetId)
                assertEquals(original.version, requireNotNull(item.sourceContentVersion))
                assertEquals(chosen.id, item.targetAssetId)
                assertEquals(chosen.version, requireNotNull(item.targetContentVersion))
                assertEquals(hash, item.contentSha256)
                assertEquals(bytes.size.toLong(), requireNotNull(item.contentBytes))
                val actual = marks().single()
                assertEquals(item.targetObjectId, actual.id)
                assertEquals(chosen.id, actual.assetId)
                assertEquals(chosen.version, actual.contentVersion)
                assertEquals(first.positionMs, actual.positionMs)
                assertEquals(first.label, actual.label)
                assertEquals(first.color, actual.color)
                assertEquals(applied.items, deliver("mark-new-task", listOf(select(first))).items)

                // The selected ID only exists at the source. Matching bytes on the target must never redirect this mark.
                val unavailable = deliver("mark-missing", listOf(select(makeMark("other"), original.id)))
                assertEquals(NativeDeliveryPlanState.partial, unavailable.state)
                assertEquals("target_asset_missing", unavailable.items.single().code)
                assertEquals(listOf(actual), marks())

                val changed = deliver("mark-replaced", listOf(select(makeMark("later")))) { file, binding ->
                    val staged = target.api.stageNativeDelivery(idempotencyKey = "preview-$device", sourceInstanceId = binding.source,
                        targetInstanceId = binding.target, packageId = binding.packageId, conflictPolicy = NativeDeliveryConflictPolicy.keep,
                        body = file.asRequestBody("application/octet-stream".toMediaType())).bodyOrThrow()
                    assertEquals(NativeDeliveryItemState.ready, staged.items.single().state)
                    val updated = uploadVideo(target, targetLibrary.id, "chosen-mark-v2.mp4", videoBytes(2), previous = chosen)
                    assertEquals(chosen.id, updated.id)
                    assertTrue(updated.version > chosen.version)
                }
                assertEquals(NativeDeliveryPlanState.partial, changed.state)
                assertEquals(NativeDeliveryItemState.conflict, changed.items.single().state)
                assertEquals("target_content_changed", changed.items.single().code)
                assertEquals(listOf(actual), marks())
            } finally { target.media.trash(target.media.asset(chosen.id)) }
        } finally { source.media.trash(source.media.asset(original.id)) }
    }

    private suspend fun uploadVideo(client: CloudClient, library: String, name: String, bytes: ByteArray, previous: Asset? = null): Asset {
        val input = UploadCreate(libraryId = library, fileName = name, propertySize = bytes.size.toLong(), mimeType = "video/mp4",
            mode = UploadCreate.Mode.ordinary, replaceAssetId = previous?.id, expectedContentVersion = previous?.version)
        val upload = client.api.createUpload("upload-$device-$name", input).bodyOrThrow()
        client.api.appendUpload(upload.id, 0L, bytes.toRequestBody("application/octet-stream".toMediaType())).bodyOrThrow()
        val completed = client.api.completeUpload(upload.id, "complete-$device-$name").bodyOrThrow()
        return client.media.asset(requireNotNull(completed.upload.assetId))
    }

    private suspend fun deliver(tag: String, items: List<NativeDeliverySelection>, replace: Boolean = false,
        beforeApply: suspend (File, DevicePackageBinding) -> Unit = { _, _ -> }): NativeDeliveryPlan {
        val task = source.api.publishNativeDelivery("publish-$device-$tag", NativeDeliveryPublish(device, targetId, items)).bodyOrThrow()
        assertTrue(peer.listDeviceApplyTasks(device, state = DeviceApplyState.pending, protocolVersion = 2, limit = 32).bodyOrThrow().items.any { it.id == task.id })
        val claimed = peer.claimDeviceApplyTask(task.id, "claim-$device-$tag", RevisionRequest(task.revision)).bodyOrThrow()
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
            beforeApply(file, binding)
            val applied = DeviceSegments.applyNative(target.api, file, binding, replace)
            assertEquals("Read the same local result after a lost commit response", applied, DeviceSegments.applyNative(target.api, file, binding, replace))
            val receipt = target.api.getNativeDelivery(requireNotNull(applied.receipt)).bodyOrThrow()
            assertTrue(receipt.items.all { !it.receiptId.isNullOrBlank() })
            val report = DeviceApplyReport(expectedRevision = claimed.revision,
                state = DeviceApplyReport.State.entries.single { it.value == applied.state }, results = listOf(applied.toOutcome()))
            peer.reportDeviceApplyTask(task.id, "report-$device-$tag", report).bodyOrThrow()
            peer.reportDeviceApplyTask(task.id, "report-$device-$tag", report).bodyOrThrow()
            assertEquals(report.results, source.api.getDeviceApplyTask(task.id).bodyOrThrow().results)
            return receipt
        } finally { file.delete() }
    }
}
