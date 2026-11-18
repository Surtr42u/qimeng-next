package media.qimeng.next.screen

import android.Manifest
import android.content.pm.PackageManager
import android.os.Build
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.selection.selectableGroup
import androidx.compose.foundation.selection.toggleable
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.core.content.ContextCompat
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LifecycleEventEffect
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import media.qimeng.next.LibraryViewModel
import media.qimeng.next.ui.component.QimengCapsuleTextField
import media.qimeng.next.ui.component.QimengPageIntro
import media.qimeng.next.ui.component.QimengTopBar
import media.qimeng.next.ui.theme.QimengDimens
import media.qimeng.next.ui.theme.softDaylightBackground
import media.qimeng.next.ui.theme.softDaylightShadow

/** Old server settings card order, geometry, text and intent; platform work stays behind runtime-api. */
@Composable
fun ServerSettingsScreen(model: LibraryViewModel) {
    val session by model.session.collectAsStateWithLifecycle()
    val busy by model.busy.collectAsStateWithLifecycle()
    val localAddress by model.localAddress.collectAsStateWithLifecycle()
    val recent by model.recentServers.collectAsStateWithLifecycle()
    val defaultEndpoint by model.defaultEndpoint.collectAsStateWithLifecycle()
    val access by model.localFileAccess.collectAsStateWithLifecycle()
    val scanState by model.localScanState.collectAsStateWithLifecycle()
    val message by model.message.collectAsStateWithLifecycle()
    val queueError by model.queueError.collectAsStateWithLifecycle()
    val connectionError by model.localConnectionError.collectAsStateWithLifecycle()
    val local = model.isLocalSession()
    var address by rememberSaveable(session?.instanceId) { mutableStateOf(if (local) recent.firstOrNull().orEmpty() else session?.baseUrl.orEmpty()) }
    var localInput by rememberSaveable { mutableStateOf(localAddress) }
    val context = LocalContext.current
    val notifications = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { }
    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) { model.refreshLocalFileAccess() }

    Column(Modifier.fillMaxSize().softDaylightBackground()) {
        QimengTopBar("服务器", onBack = model::back)
        Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(horizontal = QimengDimens.ScreenPaddingHorizontal).imePadding()) {
            QimengPageIntro(if (local) "当前使用本机模式，媒体库由这台设备管理。" else "当前连接远端服务器，可在这里管理地址和登录偏好。")
            (message ?: queueError ?: connectionError)?.let { Text(it, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.error,
                modifier = Modifier.padding(bottom = QimengDimens.SpaceL)) }
            Button(onClick = { model.prepareLogin("nas") }, enabled = !busy,
                modifier = Modifier.fillMaxWidth().padding(bottom = QimengDimens.SpaceL)) { Text("切换到 NAS / 重新寻找影库") }
            ServerSettingsCard {
                Text("服务器地址", style = MaterialTheme.typography.titleSmall)
                Text(session?.baseUrl.orEmpty().ifBlank { "—" }, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.primary)
                QimengCapsuleTextField(address, { address = it; model.clearMessage() }, placeholder = "输入新的服务器地址", singleLine = true, enabled = !busy,
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri, imeAction = ImeAction.Done),
                    keyboardActions = KeyboardActions(onDone = { model.prepareLogin("nas", address) }), modifier = Modifier.fillMaxWidth())
                SettingsHint("这里修改的是「服务器」的地址：保存后回到登录页确认；登录成功会自动记住，下次免填")
                Button(onClick = { model.prepareLogin("nas", address) }, enabled = !busy && address.isNotBlank(), modifier = Modifier.fillMaxWidth()) { Text("保存并重新登录") }
            }
            ServerSettingsCard {
                Text("本机模式", style = MaterialTheme.typography.titleSmall)
                SettingsHint(if (model.localRuntimeAvailable) "服务端跑在本机时适用：退出登录并预填 $localAddress，在登录页确认后生效" else "此安装包仅支持连接服务器，未包含本机运行模块")
                QimengCapsuleTextField(localInput, { localInput = it; model.clearMessage() }, singleLine = true, enabled = !busy && model.localRuntimeAvailable,
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri, imeAction = ImeAction.Done),
                    keyboardActions = KeyboardActions(onDone = { model.prepareLogin("local", localInput) }), modifier = Modifier.fillMaxWidth())
                TextButton(onClick = {
                    if (Build.VERSION.SDK_INT >= 33 && ContextCompat.checkSelfPermission(context, Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED)
                        notifications.launch(Manifest.permission.POST_NOTIFICATIONS)
                    model.prepareLogin("local", localInput)
                }, enabled = !busy && model.localRuntimeAvailable, modifier = Modifier.fillMaxWidth()) { Text("一键切换本机模式") }
            }
            ServerSettingsCard {
                Text("默认登录", style = MaterialTheme.typography.titleSmall)
                SettingsHint("登录页默认选中的端；实际登录成功后也会自动跟随你登的端")
                Column(Modifier.selectableGroup()) {
                    listOf("nas" to "NAS 服务器", "local" to "本机模式").forEach { (id, label) ->
                        Row(Modifier.fillMaxWidth().selectable(defaultEndpoint == id, enabled = !busy && (id != "local" || model.localRuntimeAvailable),
                            role = Role.RadioButton, onClick = { model.saveDefaultEndpoint(id) }).heightIn(min = QimengDimens.ProfileRowHeight), verticalAlignment = Alignment.CenterVertically) {
                            RadioButton(defaultEndpoint == id, null, enabled = id != "local" || model.localRuntimeAvailable)
                            Text(label, style = MaterialTheme.typography.bodyMedium, modifier = Modifier.weight(1f))
                            if ((id == "local") == local) Surface(color = MaterialTheme.colorScheme.primaryContainer,
                                contentColor = MaterialTheme.colorScheme.onPrimaryContainer, shape = RoundedCornerShape(50), modifier = Modifier.padding(start = 8.dp)) {
                                Text("正在使用", style = MaterialTheme.typography.labelSmall, modifier = Modifier.padding(horizontal = 8.dp, vertical = 2.dp))
                            }
                        }
                    }
                }
            }
            if (local) ServerSettingsCard {
                Text("仅充电时扫描", style = MaterialTheme.typography.titleSmall)
                Row(Modifier.fillMaxWidth().toggleable(scanState.chargeOnly, enabled = !busy && scanState.error == null, role = Role.Switch,
                    onValueChange = model::setChargeOnlyScan).heightIn(min = QimengDimens.ProfileRowHeight), verticalAlignment = Alignment.CenterVertically) {
                    Text("本机模式下扫描耗本机电量：未充电时点「重新扫描」会先记下，接入电源后自动开始",
                        style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.weight(1f))
                    Switch(scanState.chargeOnly, null, enabled = !busy && scanState.error == null)
                }
                scanState.error?.let { Text(it, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.labelSmall) }
            }
            ServerSettingsCard {
                Text("媒体库存储权限", style = MaterialTheme.typography.titleSmall)
                SettingsHint(when {
                    !model.localRuntimeAvailable -> "此安装包未包含本机文件访问模块"
                    access.granted -> "已授权：本机服务端可读取注册媒体目录的文件"
                    else -> "未授权：本机模式的媒体库会读不到文件，点下方按钮去系统设置打开「所有文件访问」"
                })
                access.error?.let { Text(it, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.error) }
                if (model.localRuntimeAvailable && !access.granted) TextButton(onClick = model::requestLocalFileAccess, modifier = Modifier.fillMaxWidth()) { Text("去系统设置授权") }
            }
            Text("媒体库与账号都归这台服务端管；更换地址会退出当前登录，保存后在新地址上重新登录",
                style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(bottom = QimengDimens.SpaceL))
        }
    }
}

@Composable
private fun ServerSettingsCard(content: @Composable ColumnScope.() -> Unit) {
    Surface(color = MaterialTheme.colorScheme.surface, shape = RoundedCornerShape(QimengDimens.CardCornerRadius),
        modifier = Modifier.padding(bottom = QimengDimens.SpaceL).fillMaxWidth().softDaylightShadow()) {
        Column(Modifier.padding(horizontal = 16.dp, vertical = 12.dp), verticalArrangement = Arrangement.spacedBy(8.dp), content = content)
    }
}
@Composable
private fun SettingsHint(text: String) { Text(text, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
