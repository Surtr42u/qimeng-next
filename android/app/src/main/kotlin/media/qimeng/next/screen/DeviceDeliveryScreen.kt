package media.qimeng.next.screen

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
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
            Text("PC 登记、手机领取，将本次发布的作者主表与 v2 资料归入本机影库。",style = MaterialTheme.typography.bodySmall)
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
                    Text("开启后按 v2 预检替换作者资料。人工编辑的资料保留，修订冲突可在领取记录中查看。默认保留本机资料。",
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
                val state = when(item.state) { "pending" -> "待领取"; "claimed" -> "已领取"; "applied" -> "已应用"; "partial" -> "部分完成"; "cancelled" -> "已取消"; "expired" -> "已过期"; else -> "失败" }
                Text("$state · ${item.createdAt}",style = MaterialTheme.typography.labelMedium)
                item.results.forEach { Text(segmentDescription(it), style = MaterialTheme.typography.bodySmall) }
            }
        } }
        QimengSettingsCard { Column(Modifier.padding(16.dp),verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Text("应用前提",style = MaterialTheme.typography.titleSmall)
            Text("请先连接本机影库并配置可写维护目录。设备只领取发给自己的资料包。作者资料修订冲突和 v2 未能明确匹配的项保留在报告中。",style = MaterialTheme.typography.bodySmall)
        } }
    }
}

private fun segmentDescription(result: DeviceSegmentResult): String {
    val kind = if (result.kind == "v2Backup") "v2 资料" else "作者主表"
    val state = when (result.state) { "applied" -> "已应用"; "partial" -> "部分应用"; "skipped" -> "已跳过"; else -> "失败" }
    val reason = when (result.reason) {
        "v2_review_required" -> "请核对本机预检的匹配和冲突"
        "migration_profile_changed" -> "预检后的作者资料已变更"
        "v2_profiles_protected" -> "已保留受保护的本机资料"
        "v2_profiles_candidate" -> "部分资料仅作为候选保留"
        "v2_profiles_protected_and_candidate" -> "已保留受保护的本机资料和待核对的候选"
        else -> result.reason
    }
    return listOfNotNull(kind, state, reason, result.receipt?.let { "本机回执 $it" }).joinToString(" · ")
}
