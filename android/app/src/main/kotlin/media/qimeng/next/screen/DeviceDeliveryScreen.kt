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
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import media.qimeng.next.DeviceDeliveryViewModel
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
                Switch(settings.background,model::toggle,enabled = !busy && (settings.background || local && settings.configured))
            }
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Button(onClick = model::applyNow,enabled = !busy && settings.configured && local,shape = RoundedCornerShape(12.dp)) { Text("立即应用") }
                OutlinedButton(onClick = { model.save(address,device,secret) { secret = "" } },enabled = !busy && secret.isNotBlank() && device.isNotBlank(),shape = RoundedCornerShape(12.dp)) { Text("保存设置") }
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
                item.results.forEach { Text(it,style = MaterialTheme.typography.bodySmall) }
            }
        } }
        QimengSettingsCard { Column(Modifier.padding(16.dp),verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Text("应用前提",style = MaterialTheme.typography.titleSmall)
            Text("请先连接本机影库并配置可写维护目录。设备只领取发给自己的资料包。作者资料修订冲突和 v2 未能明确匹配的项保留在报告中。",style = MaterialTheme.typography.bodySmall)
        } }
    }
}
