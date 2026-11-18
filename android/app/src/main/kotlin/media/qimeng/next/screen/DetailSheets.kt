package media.qimeng.next.screen

import android.content.ClipData
import android.content.ClipboardManager
import android.widget.Toast
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.*
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import media.qimeng.next.R
import media.qimeng.next.model.Asset
import media.qimeng.next.model.Author
import media.qimeng.next.ui.component.QIMENG_GLASS_TINT_ALPHA
import media.qimeng.next.ui.glass.*
import media.qimeng.next.ui.icon.*
import media.qimeng.next.ui.theme.QimengDimens
import java.time.Instant
import java.time.ZoneId
import java.util.Locale

private fun detailDate(iso: String?): String = iso?.let { Instant.parse(it).atZone(ZoneId.systemDefault()).toLocalDate() }
    ?.let { "${it.monthValue}-${it.dayOfMonth}" }.orEmpty()
private fun detailSize(bytes: Long): String = when {
    bytes >= (1L shl 30) -> String.format(Locale.US, "%.1f GB", bytes / 1073741824.0)
    bytes >= (1L shl 20) -> String.format(Locale.US, "%.1f MB", bytes / 1048576.0)
    bytes >= (1L shl 10) -> String.format(Locale.US, "%.0f KB", bytes / 1024.0)
    else -> "$bytes B"
}
private fun detailTypeLabel(kind: String, fileName: String): String {
    val label = when (kind) { "image" -> "图片"; "video" -> "视频"; "animated_image" -> "动图"; else -> kind }
    val extension = fileName.removePrefix(".").substringAfterLast('.', "").lowercase()
    return if (extension.isEmpty()) label else "$label · $extension"
}
internal fun detailDuration(ms: Long?): String? {
    val seconds = ms?.takeIf { it > 0 }?.div(1000) ?: return null
    val minutes = seconds / 60
    val tail = (seconds % 60).toString().padStart(2, '0')
    return if (minutes >= 60) "${minutes / 60}:${(minutes % 60).toString().padStart(2, '0')}:$tail" else "$minutes:$tail"
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun DetailInfoSheet(asset: Asset, onDismiss: () -> Unit) {
    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = QimengDimens.ScreenPaddingHorizontal)
                .padding(bottom = 24.dp),
        ) {
            Text(
                text = stringResource(R.string.detail_info_sheet_title),
                style = MaterialTheme.typography.titleLarge,
            )
            InfoRow(
                label = stringResource(R.string.detail_info_filename),
                value = asset.name,
                copyValue = asset.name,
            )
            if (asset.originalName != asset.name) InfoRow("来源原名", asset.originalName)
            asset.cosWorkName?.takeIf { it.isNotEmpty() }?.let { work ->
                InfoRow(label = stringResource(R.string.detail_info_work), value = work)
            }
            asset.sourceName?.takeIf { it.isNotEmpty() }?.let { source ->
                InfoRow(label = stringResource(R.string.detail_info_source), value = source)
            }
            detailDate(asset.fileDate).takeIf { it.isNotEmpty() }?.let { date ->
                InfoRow(label = stringResource(R.string.detail_info_date), value = date)
            }
            InfoRow(
                label = stringResource(R.string.detail_info_size),
                value = detailSize(asset.bytes),
            )
            InfoRow(
                label = stringResource(R.string.detail_info_type),
                value = detailTypeLabel(asset.mediaType, asset.name),
            )
            val w = asset.width
            val h = asset.height
            if (w != null && h != null && w > 0 && h > 0) {
                InfoRow(
                    label = stringResource(R.string.detail_info_resolution),
                    value = stringResource(R.string.detail_meta_resolution, w, h),
                )
            }
            detailDuration(asset.durationMs)?.let { duration ->
                InfoRow(label = stringResource(R.string.detail_info_duration), value = duration)
            }
            InfoRow(
                label = stringResource(R.string.detail_info_directory),
                value = asset.relativePath.substringBeforeLast('/', "").ifEmpty { "/" },
            )
            asset.relativePath.takeIf { it.isNotEmpty() }?.let { relPath ->
                InfoRow(label = stringResource(R.string.detail_info_path), value = relPath)
            }
        }
    }
}

@Composable
private fun InfoRow(label: String, value: String, copyValue: String? = null) {
    val context = LocalContext.current
    val copiedText = stringResource(R.string.detail_info_filename_copied)
    Column(modifier = Modifier.padding(top = QimengDimens.SpaceM)) {
        Text(
            text = label,
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                text = value,
                style = MaterialTheme.typography.bodyLarge,
                modifier = Modifier
                    .weight(1f)
                    .padding(top = QimengDimens.SpaceXS),
            )
            if (copyValue != null) {
                IconButton(
                    onClick = {
                        val clipboard = context.getSystemService(ClipboardManager::class.java)
                        if (clipboard != null) {
                            clipboard.setPrimaryClip(ClipData.newPlainText(label, copyValue))
                            Toast.makeText(context, copiedText, Toast.LENGTH_SHORT).show()
                        }
                    },
                ) {
                    Icon(
                        imageVector = CopyIcon,
                        contentDescription = stringResource(R.string.detail_info_copy_filename),
                        tint = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
        }
    }
}


@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun DetailAuthorSheet(
    authors: List<Author>,
    followPending: Boolean,
    onToggleFollow: (String) -> Unit,
    onOpenAuthor: (authorId: String, displayName: String) -> Unit,
    onEdit: () -> Unit,
    onDismiss: () -> Unit,
) {
    ModalBottomSheet(
        onDismissRequest = onDismiss,
        dragHandle = null,
        containerColor = MaterialTheme.colorScheme.surfaceContainerLow,
        contentWindowInsets = { BottomSheetDefaults.standardWindowInsets },
        properties = ModalBottomSheetProperties(
            shouldDismissOnBackPress = true,
            shouldDismissOnClickOutside = true,
        ),
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .padding(top = 18.dp)
                .padding(horizontal = 20.dp)
                .padding(bottom = 24.dp),
        ) {
            Box(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(bottom = 12.dp),
                contentAlignment = Alignment.Center,
            ) {
                Surface(
                    color = MaterialTheme.colorScheme.onSurfaceVariant.copy(alpha = 0.3f),
                    shape = RoundedCornerShape(2.dp),
                    modifier = Modifier.size(width = 36.dp, height = 4.dp),
                ) {}
            }

            Row(
                modifier = Modifier.fillMaxWidth(),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.SpaceBetween,
            ) {
                Text(
                    text = stringResource(R.string.detail_authors_title),
                    style = MaterialTheme.typography.titleLarge,
                    fontWeight = FontWeight.Bold,
                    color = MaterialTheme.colorScheme.onSurface,
                )
                if (authors.isNotEmpty()) {
                    Surface(
                        color = MaterialTheme.colorScheme.primaryContainer,
                        shape = RoundedCornerShape(12.dp),
                    ) {
                        Text(
                            text = "${authors.size} 位创作者",
                            style = MaterialTheme.typography.labelSmall,
                            fontWeight = FontWeight.Bold,
                            color = MaterialTheme.colorScheme.onPrimaryContainer,
                            modifier = Modifier.padding(horizontal = 8.dp, vertical = 4.dp),
                        )
                    }
                }
            }

            if (authors.isEmpty()) {
                Surface(
                    shape = RoundedCornerShape(16.dp),
                    color = MaterialTheme.colorScheme.surface,
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(top = QimengDimens.SpaceM),
                ) {
                    Column(
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(20.dp),
                        horizontalAlignment = Alignment.CenterHorizontally,
                    ) {
                        Text(
                            text = stringResource(R.string.detail_jump_empty),
                            style = MaterialTheme.typography.bodyMedium,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                }
            } else {
                val cosSuffix = stringResource(R.string.detail_author_cos_suffix)
                val enterHomeLabel = stringResource(R.string.detail_author_open_collection)
                authors.forEach { author ->
                    Surface(
                        shape = RoundedCornerShape(16.dp),
                        color = MaterialTheme.colorScheme.surface,
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(top = QimengDimens.SpaceM),
                    ) {
                        Column(modifier = Modifier.padding(16.dp)) {
                            Row(
                                modifier = Modifier.fillMaxWidth(),
                                verticalAlignment = Alignment.CenterVertically,
                            ) {
                                AuthorAvatar(author.id, author.name, 44.dp)
                                Spacer(modifier = Modifier.width(12.dp))
                                Column(modifier = Modifier.weight(1f)) {
                                    Row(verticalAlignment = Alignment.CenterVertically) {
                                        Text(
                                            text = author.name,
                                            style = MaterialTheme.typography.titleMedium,
                                            fontWeight = FontWeight.Bold,
                                            color = MaterialTheme.colorScheme.onSurface,
                                        )
                                        if (author.kind == "cos") {
                                            Spacer(modifier = Modifier.width(6.dp))
                                            Surface(
                                                shape = RoundedCornerShape(6.dp),
                                                color = MaterialTheme.colorScheme.tertiaryContainer,
                                            ) {
                                                Text(
                                                    text = cosSuffix,
                                                    style = MaterialTheme.typography.labelSmall,
                                                    fontWeight = FontWeight.Bold,
                                                    color = MaterialTheme.colorScheme.onTertiaryContainer,
                                                    modifier = Modifier.padding(horizontal = 5.dp, vertical = 1.dp),
                                                )
                                            }
                                        }
                                    }
                                }
                                FollowButton(
                                    followed = author.followed,
                                    enabled = !followPending,
                                    onClick = { onToggleFollow(author.id) },
                                )
                            }

                            Spacer(modifier = Modifier.height(12.dp))
                            HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.5f))
                            Spacer(modifier = Modifier.height(10.dp))

                            Row(
                                modifier = Modifier
                                    .fillMaxWidth()
                                    .clickable {
                                        onOpenAuthor(author.id, author.name)
                                        onDismiss()
                                    },
                                verticalAlignment = Alignment.CenterVertically,
                                horizontalArrangement = Arrangement.SpaceBetween,
                            ) {
                                Text(
                                    text = enterHomeLabel,
                                    style = MaterialTheme.typography.bodyMedium,
                                    fontWeight = FontWeight.Medium,
                                    color = MaterialTheme.colorScheme.primary,
                                )
                                Icon(
                                    imageVector = ChevronRightIcon,
                                    contentDescription = null,
                                    tint = MaterialTheme.colorScheme.primary,
                                    modifier = Modifier.size(18.dp),
                                )
                            }
                        }
                    }
                }
            }

            Surface(
                shape = RoundedCornerShape(16.dp),
                color = MaterialTheme.colorScheme.surface,
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(top = QimengDimens.SpaceM)
                    .clickable {
                        onEdit()
                        onDismiss()
                    },
            ) {
                Row(
                    modifier = Modifier.padding(16.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Surface(
                        shape = CircleShape,
                        color = MaterialTheme.colorScheme.primaryContainer,
                        modifier = Modifier.size(36.dp),
                    ) {
                        Box(contentAlignment = Alignment.Center) {
                            Icon(
                                imageVector = EditPencilIcon,
                                contentDescription = null,
                                tint = MaterialTheme.colorScheme.onPrimaryContainer,
                                modifier = Modifier.size(18.dp),
                            )
                        }
                    }
                    Spacer(modifier = Modifier.width(12.dp))
                    Column(modifier = Modifier.weight(1f)) {
                        Text(
                            text = stringResource(R.string.asset_edit_title),
                            style = MaterialTheme.typography.titleMedium,
                            fontWeight = FontWeight.Bold,
                            color = MaterialTheme.colorScheme.onSurface,
                        )
                        Spacer(modifier = Modifier.height(2.dp))
                        Text(
                            text = "配置关联作者与来源，自动写回 NAS 作品 TXT",
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                    Icon(
                        imageVector = ChevronRightIcon,
                        contentDescription = null,
                        tint = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.size(18.dp),
                    )
                }
            }
        }
    }
}

@Composable
private fun FollowButton(followed: Boolean, enabled: Boolean, onClick: () -> Unit) {
    val interaction = rememberPressScaleSource()
    GlassSurface(
        shape = RoundedCornerShape(QimengDimens.PillCornerRadius),
        compact = true,
        tintOverlay = if (!followed) {
            MaterialTheme.colorScheme.primary.copy(alpha = QIMENG_GLASS_TINT_ALPHA)
        } else {
            null
        },
        modifier = Modifier
            .pressScale(interaction)
            .clickable(interactionSource = interaction, indication = null, enabled = enabled, onClick = onClick),
    ) {
        Text(
            text = stringResource(if (followed) R.string.detail_followed else R.string.detail_follow),
            style = MaterialTheme.typography.labelLarge,
            color = if (followed) {
                MaterialTheme.colorScheme.onSurface
            } else {
                MaterialTheme.colorScheme.primary
            },
            modifier = Modifier.padding(
                horizontal = QimengDimens.ChipHorizontalPadding,
                vertical = QimengDimens.SpaceS,
            ),
        )
    }
}
