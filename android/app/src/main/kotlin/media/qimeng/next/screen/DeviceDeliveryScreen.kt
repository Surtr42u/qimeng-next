package media.qimeng.next.screen

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import media.qimeng.next.DeviceDeliveryViewModel
import media.qimeng.next.model.DeviceSegmentResult
import media.qimeng.next.ui.ErrorNotice
import media.qimeng.next.ui.component.QimengCapsuleTextField
import media.qimeng.next.ui.component.QimengSettingsCard
import media.qimeng.next.ui.icon.BackupRestoreIcon

@Composable
internal fun DeviceDeliveryScreen(model: DeviceDeliveryViewModel = viewModel()) {
    val settings by model.settings.collectAsStateWithLifecycle()
    val session by model.session.collectAsStateWithLifecycle()
    val list by model.recent.collectAsStateWithLifecycle()
    val receipt by model.receipt.collectAsStateWithLifecycle()
    val busy by model.busy.collectAsStateWithLifecycle()
    val error by model.error.collectAsStateWithLifecycle()
    var device by rememberSaveable(settings.deviceId) { mutableStateOf(settings.deviceId) }
    var address by rememberSaveable(settings.address) { mutableStateOf(settings.address) }
    var secret by remember(session, settings.configured) { mutableStateOf("") }
    var background by rememberSaveable(session?.instanceId, settings.configured, settings.background) { mutableStateOf(settings.background) }
    var replaceProfiles by rememberSaveable(session?.instanceId, settings.replaceAuthorProfiles) { mutableStateOf(settings.replaceAuthorProfiles) }
    val local = model.local(session)
    Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(horizontal = 16.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp)) {
        MaintenanceCard(BackupRestoreIcon,"设备结果包") {
            Text("PC 或 NAS 选择作者资料、TXT 片段或指定视频版本的时间轴标记。手机领取后将它们归入指定的 Next 本机影库。",style = MaterialTheme.typography.bodySmall)
            if (local) session?.instanceId?.let { id ->
                DeviceTargetValue("本机影库标识", id)
                DeviceTargetVideoPicker(id, model, busy)
            }
            Text("设备标签",style = MaterialTheme.typography.labelMedium)
            QimengCapsuleTextField(device,{ device = it },Modifier.fillMaxWidth(),placeholder = "如 phone-main",singleLine = true,enabled = !busy)
            Text("设备 AI 令牌",style = MaterialTheme.typography.labelMedium)
            QimengCapsuleTextField(secret,{ secret = it },Modifier.fillMaxWidth(),placeholder = if (settings.configured) "已配置；输入新令牌可替换" else "只显示一次的专用令牌",
                enabled = !busy,singleLine = true,visualTransformation = PasswordVisualTransformation(),keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password))
            Text("领取服务端地址（可选）",style = MaterialTheme.typography.labelMedium)
            QimengCapsuleTextField(address,{ address = it },Modifier.fillMaxWidth(),placeholder = "留空使用当前服务端",enabled = !busy,singleLine = true,
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri))
            if (local) Button(onClick = { model.issue(device) },enabled = !busy && device.isNotBlank(),shape = RoundedCornerShape(12.dp),modifier = Modifier.fillMaxWidth()) { Text("生成本机设备令牌") }
            if (settings.configured) OutlinedButton(onClick = model::clear,enabled = !busy,shape = RoundedCornerShape(12.dp),modifier = Modifier.fillMaxWidth()) { Text("清除设备令牌") }
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                Column(Modifier.weight(1f)) { Text("后台领取",style = MaterialTheme.typography.titleSmall); Text("每 15 分钟尝试一次，系统省电可能延迟。",style = MaterialTheme.typography.bodySmall) }
                Switch(background, { background = it }, modifier = Modifier.testTag("device-background"),
                    enabled = !busy && (background || local && (settings.configured || secret.isNotBlank())))
            }
            Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                    Text("应用时覆盖作者资料", style = MaterialTheme.typography.titleSmall)
                    Text("默认保留已有资料。开启后可更新从同一 Next 来源导入、且在本机没有修改的资料。人工编辑或清空的内容受保护。",
                        style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
                Switch(checked = replaceProfiles, onCheckedChange = { replaceProfiles = it }, enabled = !busy, modifier = Modifier.testTag("device-replace-author-profiles"))
            }
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Button(onClick = { model.save(address, device, secret, background, replaceProfiles, applyNow = true) { secret = "" } },
                    enabled = !busy && (settings.configured || secret.isNotBlank()) && device.isNotBlank() && local,
                    shape = RoundedCornerShape(12.dp), modifier = Modifier.weight(1f)) { Text("立即应用") }
                OutlinedButton(onClick = { model.save(address, device, secret, background, replaceProfiles, applyNow = false) { secret = "" } },
                    enabled = !busy && device.isNotBlank(), shape = RoundedCornerShape(12.dp), modifier = Modifier.weight(1f)) { Text("保存设置") }
            }
            error?.let { ErrorNotice(it) }
            settings.notice?.let { Text(it,style = MaterialTheme.typography.bodySmall) }
        }
        QimengSettingsCard { Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) { Text("最近设备任务",Modifier.weight(1f),style = MaterialTheme.typography.titleSmall)
                TextButton(onClick = model::refresh,enabled = !busy && settings.configured) { Text("刷新") } }
            if (list.isNullOrEmpty()) Text(if (list == null) "可读取此设备最近的领取记录。" else "暂无设备任务。",style = MaterialTheme.typography.bodySmall)
            list.orEmpty().forEach { item ->
                val state = when(item.state) { "pending" -> "待领取"; "claimed" -> "已领取"; "applied" -> "已应用"; "partial" -> "部分完成"; "cancelled" -> "已取消"; "expired" -> "已过期"; "legacy_local" -> "旧版任务的本机留存回执"; else -> "失败" }
                Text(listOf(state, item.createdAt).filter(String::isNotBlank).joinToString(" · "), style = MaterialTheme.typography.labelMedium)
                item.results.forEach { result ->
                    Text(segmentDescription(result), style = MaterialTheme.typography.bodySmall)
                    if (local && result.native?.let { it.target == session?.instanceId } == true && result.receipt != null) {
                        val items = receipt?.takeIf { it.first == result.receipt }?.second
                        TextButton(onClick = { model.showReceipt(result) }, enabled = !busy) { Text(if (items == null) "查看逐项回执" else "收起回执") }
                        items.orEmpty().forEach { entry ->
                            val block = when (entry.block) { "x" -> "X 资料"; "basic" -> "基本资料"; "accounts" -> "其他账号"; else -> null }
                            val state = when (entry.state) { "applied" -> "已应用"; "preserved" -> "已保留"; "conflict" -> "冲突"; else -> "待提交" }
                            val kind = when (entry.kind) { "timelineMark" -> "时间轴标记"; "authorList" -> "TXT 片段"; else -> block }
                            Text(listOfNotNull(entry.label, kind, state, deliveryReason(entry.reason), entry.receipt).joinToString(" · "), style = MaterialTheme.typography.bodySmall)
                            entry.video?.let { video ->
                                Text("来源视频 ${video.sourceId ?: "未返回"} · 内容版本 ${video.sourceVersion ?: "未返回"}\n目标视频 ${video.targetId ?: "未返回"} · 内容版本 ${video.targetVersion ?: "未返回"}", style = MaterialTheme.typography.bodySmall)
                                if (video.sha256 != null && video.bytes != null) SelectionContainer { Text("${video.bytes} 字节 · SHA-256 ${video.sha256}", style = MaterialTheme.typography.bodySmall) }
                            }
                        }
                    }
                }
            }
        } }
        QimengSettingsCard { Column(Modifier.padding(16.dp),verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Text("应用前提",style = MaterialTheme.typography.titleSmall)
            Text("请先连接本机影库并配置可写维护目录。设备只领取发给本设备和本机影库的原生包。最近设备任务可查看每项资料的处理结果。",style = MaterialTheme.typography.bodySmall)
        } }
    }
}

private fun segmentDescription(result: DeviceSegmentResult): String {
    val kind = when (result.kind) { "nativeSelection" -> "Next 原生资料"; "v2Backup" -> "旧版 v2 资料"; "masterTxt" -> "旧版作者主表"; else -> result.kind }
    val state = when (result.state) { "applied" -> "已应用"; "partial" -> "部分应用"; "skipped" -> "已跳过"; else -> "失败" }
    return listOfNotNull(kind, state, deliveryReason(result.reason), result.receipt?.let { "本机回执 $it" }).joinToString(" · ")
}

private fun deliveryReason(code: String?): String? = when (code) {
        "keep_existing" -> "按选择保留已有资料"
        "already_current" -> "与已收资料相同"
        "human_protected" -> "保留人工编辑或清空的资料"
        "identity_conflict" -> "目标身份未能明确匹配"
        "target_changed" -> "预检后本机资料已变化"
        "source_revision_conflict" -> "来源资料修订冲突"
        "native_partial" -> "请查看逐项处理结果"
        "native_policy_changed" -> "已暂存的原生包使用不同的冲突处理选择"
        "native_policy_frozen" -> "原生包已按此前的选择暂存"
        "v2_review_required" -> "请核对本机预检的匹配和冲突"
        "migration_profile_changed" -> "预检后的作者资料已变更"
        "v2_profiles_protected" -> "已保留受保护的本机资料"
        "v2_profiles_candidate" -> "部分资料仅作为候选保留"
        "v2_profiles_protected_and_candidate" -> "已保留受保护的本机资料和待核对的候选"
        else -> code
    }
