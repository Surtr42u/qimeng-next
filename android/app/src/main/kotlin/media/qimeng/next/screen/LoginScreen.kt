package media.qimeng.next.screen

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.compose.LifecycleResumeEffect
import media.qimeng.next.LibraryViewModel
import media.qimeng.next.R
import media.qimeng.next.ui.component.QimengCapsuleTextField
import media.qimeng.next.ui.theme.QimengDimens
import media.qimeng.next.ui.theme.qimengFilledButtonColors
import media.qimeng.next.ui.theme.softDaylightShadow
import media.qimeng.next.model.ServerDiscoveryState

/** The previous LoginScreen's layout and presentation, connected to the new runtime/API boundary. */
@Composable
fun LoginScreen(model: LibraryViewModel) {
    val defaultEndpoint by model.defaultEndpoint.collectAsStateWithLifecycle()
    val stagedEndpoint by model.stagedLoginEndpoint.collectAsStateWithLifecycle()
    val stagedAddress by model.stagedLoginAddress.collectAsStateWithLifecycle()
    var selected by rememberSaveable { mutableStateOf((stagedEndpoint ?: defaultEndpoint)?.let { if (it == "local" && !model.localRuntimeAvailable) "nas" else it }) }
    var address by rememberSaveable { mutableStateOf(stagedAddress.orEmpty()) }
    var addressEdited by rememberSaveable { mutableStateOf(stagedAddress != null) }
    var showManual by rememberSaveable { mutableStateOf(false) }
    // Password is transient composition state: never a SavedStateHandle, queue field or remembered address.
    var password by remember { mutableStateOf("") }
    val addresses by model.recentServers.collectAsStateWithLifecycle()
    val busy by model.busy.collectAsStateWithLifecycle()
    val message by model.message.collectAsStateWithLifecycle()
    val settingsError by model.settingsError.collectAsStateWithLifecycle()
    val discovery by model.discovery.collectAsStateWithLifecycle()
    LifecycleResumeEffect(selected) {
        if (selected == "nas") model.discoverServers()
        onPauseOrDispose { model.stopDiscovery() }
    }
    LaunchedEffect(addresses) { if (address.isEmpty()) address = addresses.firstOrNull().orEmpty() }
    LaunchedEffect(discovery.finished, discovery.candidates) {
        if (!addressEdited && discovery.finished && discovery.candidates.size == 1) address = discovery.candidates.single().address
    }
    LoginContent(selected, address, password, showManual, addresses, busy, model.localRuntimeAvailable,
        message ?: settingsError, discovery, model::discoverServers,
        onMode = { selected = it; model.clearMessage() },
        onAddress = { address = it; addressEdited = true; model.clearMessage() },
        onPassword = { password = it; model.clearMessage() },
        onManual = { showManual = !showManual },
        onSubmit = {
            if (!busy && (selected != "nas" || !discovery.searching)) when (selected) {
                "local" -> model.loginLocal(password)
                "nas" -> model.login(address, password, discovery.candidates.find { it.address == address }?.instanceId)
                else -> Unit
            }
        })
}

@OptIn(ExperimentalMaterial3ExpressiveApi::class)
@Composable
internal fun LoginContent(selected: String?, address: String, password: String, showManual: Boolean,
    addresses: List<String>, submitting: Boolean, localAvailable: Boolean, error: String?,
    discovery: ServerDiscoveryState, onDiscover: () -> Unit,
    onMode: (String) -> Unit, onAddress: (String) -> Unit, onPassword: (String) -> Unit,
    onManual: () -> Unit, onSubmit: () -> Unit) {
    Surface(modifier = Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) {
        Column(Modifier.fillMaxSize().statusBarsPadding().windowInsetsPadding(WindowInsets.ime.union(WindowInsets.navigationBars))) {
            Column(Modifier.weight(1f).verticalScroll(rememberScrollState()).padding(24.dp), verticalArrangement = Arrangement.Center) {
                Text(stringResource(R.string.login_brand), style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.primary)
                Spacer(Modifier.height(12.dp))
                Text(stringResource(R.string.login_heading), style = MaterialTheme.typography.headlineLarge)
                Spacer(Modifier.height(8.dp))
                Text(stringResource(R.string.login_intro), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                Spacer(Modifier.height(28.dp))
                Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                    ModeButton(stringResource(R.string.login_option_local), stringResource(R.string.login_local_caption),
                        selected == "local", !submitting && localAvailable, Modifier.weight(1f)) { onMode("local") }
                    ModeButton(stringResource(R.string.login_option_server), stringResource(R.string.login_nas_caption),
                        selected == "nas", !submitting, Modifier.weight(1f)) { onMode("nas") }
                }
                Spacer(Modifier.height(20.dp))
                val shape = RoundedCornerShape(24.dp)
                Surface(modifier = Modifier.fillMaxWidth().softDaylightShadow(shape), shape = shape,
                    color = MaterialTheme.colorScheme.surface,
                    border = BorderStroke(1.dp, MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.45f))) {
                    Column(Modifier.padding(20.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                        when (selected) {
                            "local" -> {
                                Text(stringResource(R.string.login_local_title), style = MaterialTheme.typography.titleMedium)
                                Text(stringResource(R.string.login_local_description), style = MaterialTheme.typography.bodyMedium,
                                    color = MaterialTheme.colorScheme.onSurfaceVariant)
                            }
                            "nas" -> {
                                Row(verticalAlignment = Alignment.CenterVertically) {
                                    Text(stringResource(if (discovery.searching) R.string.login_searching else R.string.login_nas_title), Modifier.weight(1f), style = MaterialTheme.typography.titleMedium)
                                    if (discovery.searching) LoadingIndicator(Modifier.size(QimengDimens.ButtonLoadingIndicatorSize))
                                    else TextButton(onClick = onDiscover, enabled = !submitting) { Text(stringResource(R.string.login_refresh)) }
                                }
                                Text(stringResource(R.string.login_search_hint), style = MaterialTheme.typography.bodySmall,
                                    color = MaterialTheme.colorScheme.onSurfaceVariant)
                                val found = discovery.candidates.map { it.address }
                                val available = (found + addresses).distinct()
                                available.forEach { candidate -> AddressRow(candidate, candidate == address, !submitting, candidate in found) { onAddress(candidate) } }
                                if (discovery.finished && found.isEmpty()) Text(discovery.error ?: stringResource(R.string.login_search_empty),
                                    style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                                TextButton(onClick = onManual, enabled = !submitting) {
                                    Text(stringResource(if (showManual) R.string.login_manual_hide else R.string.login_manual))
                                }
                                if (showManual) QimengCapsuleTextField(address, onAddress,
                                    placeholder = stringResource(R.string.login_server_url_placeholder), singleLine = true,
                                    enabled = !submitting, modifier = Modifier.fillMaxWidth().testTag("login-address"),
                                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri, imeAction = ImeAction.Next))
                                if (address.isNotEmpty() && address !in available && !showManual) Text(address, style = MaterialTheme.typography.bodySmall)
                            }
                            else -> Text(stringResource(R.string.login_choose_mode), style = MaterialTheme.typography.bodyMedium)
                        }
                    }
                }
                Spacer(Modifier.height(24.dp))
                QimengCapsuleTextField(password, onPassword, placeholder = stringResource(R.string.login_password_label), singleLine = true,
                    enabled = !submitting, visualTransformation = PasswordVisualTransformation(),
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password, imeAction = ImeAction.Done),
                    keyboardActions = KeyboardActions(onDone = { onSubmit() }), modifier = Modifier.fillMaxWidth().testTag("login-password"))
                error?.let {
                    Spacer(Modifier.height(12.dp))
                    Text(it, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodyMedium, modifier = Modifier.testTag("login-error"))
                }
                Spacer(Modifier.height(20.dp))
                Button(onClick = onSubmit, enabled = !submitting && selected != null && (selected != "nas" || !discovery.searching),
                    colors = qimengFilledButtonColors(), modifier = Modifier.fillMaxWidth()) {
                    if (submitting) LoadingIndicator(Modifier.size(QimengDimens.ButtonLoadingIndicatorSize))
                    Text(stringResource(if (submitting) R.string.login_connecting else R.string.login_submit))
                }
                Spacer(Modifier.height(16.dp))
                Text(stringResource(R.string.login_footer), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
    }
}

@Composable
private fun ModeButton(title: String, caption: String, selected: Boolean, enabled: Boolean, modifier: Modifier, onClick: () -> Unit) {
    val shape = RoundedCornerShape(20.dp)
    Surface(modifier = modifier.selectable(selected, enabled = enabled, role = Role.RadioButton, onClick = onClick), shape = shape,
        color = if (selected) MaterialTheme.colorScheme.primaryContainer else MaterialTheme.colorScheme.surfaceContainerLow,
        border = if (selected) BorderStroke(1.dp, MaterialTheme.colorScheme.primary) else null) {
        Column(Modifier.padding(18.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Text(title, style = MaterialTheme.typography.titleMedium,
                color = if (selected) MaterialTheme.colorScheme.onPrimaryContainer else MaterialTheme.colorScheme.onSurface)
            Text(caption, style = MaterialTheme.typography.bodySmall,
                color = if (selected) MaterialTheme.colorScheme.onPrimaryContainer else MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}

@Composable
private fun AddressRow(address: String, selected: Boolean, enabled: Boolean, discovered: Boolean, onClick: () -> Unit) {
    Surface(modifier = Modifier.fillMaxWidth().selectable(selected, enabled = enabled, role = Role.RadioButton, onClick = onClick),
        shape = RoundedCornerShape(14.dp),
        color = if (selected) MaterialTheme.colorScheme.secondaryContainer else MaterialTheme.colorScheme.surfaceContainerLow) {
        Column(Modifier.padding(12.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text(stringResource(if (discovered) R.string.login_address_found else R.string.login_address_saved), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            Text(address, style = MaterialTheme.typography.bodyMedium)
        }
    }
}
