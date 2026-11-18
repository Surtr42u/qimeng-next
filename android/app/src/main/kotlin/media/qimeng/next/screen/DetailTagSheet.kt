package media.qimeng.next.screen

import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.tween
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.toggleable
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ExperimentalMaterial3ExpressiveApi
import androidx.compose.material3.Icon
import androidx.compose.material3.LoadingIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import media.qimeng.next.R
import androidx.compose.ui.platform.testTag
import androidx.compose.runtime.saveable.rememberSaveable
import media.qimeng.next.ui.component.QimengCapsuleTextField
import media.qimeng.next.ui.icon.ClearIcon
import media.qimeng.next.ui.theme.QimengDimens
import media.qimeng.next.ui.theme.qimengFilledButtonColors


private val TAG_CLEAR_ICON_SIZE = 16.dp

private val TAG_SAVE_PROGRESS_SIZE = QimengDimens.ButtonLoadingIndicatorSize

private val TAG_SHEET_TITLE_TEXT_SIZE = 18.sp

private val TAG_SECTION_TITLE_TEXT_SIZE = 13.sp

private val TAG_ADD_BUTTON_WIDTH = 52.dp

private val TAG_ADD_ROW_SPACING = 10.dp

private val TAG_ADD_ROW_TOP_SPACING = 8.dp

private val TAG_SHEET_CONTENT_TOP_PADDING = 18.dp

private val TAG_POOL_CHIP_HEIGHT = 36.dp
private val TAG_POOL_CHIP_TEXT_SIZE = 14.sp

private val TAG_CURRENT_TAGS_HEIGHT = 76.dp

private const val TAG_SELECTION_TRANSITION_MS = 120

private const val TAG_SHEET_HEIGHT_FRACTION = 0.8f

private val TAG_FOOTER_TOP_SPACING = 18.dp

@OptIn(
    ExperimentalMaterial3Api::class,
    ExperimentalMaterial3ExpressiveApi::class,
    ExperimentalLayoutApi::class,
)
@Composable
internal fun DetailTagManageSheet(
    pool: List<String>,
    selectedTagIds: List<String>,
    savingTags: Boolean,
    saveEnabled: Boolean,
    onToggleTag: (String) -> Unit,
    onUnbindTag: (String) -> Unit,
    onCreateTag: (name: String, onCreated: () -> Unit) -> Unit,
    onSave: () -> Unit,
    onDismiss: () -> Unit,
    poolFooter: @Composable () -> Unit,
    feedback: @Composable () -> Unit,
) {
    val sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)
    var draft by rememberSaveable { mutableStateOf("") }
    val maxSheetHeight = LocalConfiguration.current.screenHeightDp.dp * TAG_SHEET_HEIGHT_FRACTION
    ModalBottomSheet(
        onDismissRequest = onDismiss,
        sheetState = sheetState,
        dragHandle = null,
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .heightIn(max = maxSheetHeight)
                .padding(top = TAG_SHEET_CONTENT_TOP_PADDING)
                .padding(horizontal = QimengDimens.DetailSheetPaddingHorizontal)
                .padding(bottom = 24.dp),
        ) {
            Row(
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(QimengDimens.SpaceM),
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(bottom = QimengDimens.SpaceL),
            ) {
                Text(
                    text = stringResource(R.string.detail_tag_sheet_title),
                    style = MaterialTheme.typography.titleLarge.copy(
                        fontSize = TAG_SHEET_TITLE_TEXT_SIZE,
                        fontWeight = FontWeight.Bold,
                    ),
                    modifier = Modifier.weight(1f),
                )
                Text(
                    text = stringResource(R.string.detail_tag_selected_count, selectedTagIds.size),
                    style = MaterialTheme.typography.labelLarge,
                    color = MaterialTheme.colorScheme.primary,
                )
            }
            SheetCurrentTags(
                pool = pool,
                selectedTagIds = selectedTagIds,
                onUnbindTag = onUnbindTag,
            )
            Spacer(modifier = Modifier.height(QimengDimens.SpaceL))
            Column(
                modifier = Modifier
                    .weight(1f, fill = false)
                    .fillMaxWidth()
                    .verticalScroll(rememberScrollState()),
            ) {
                SheetTagPool(
                    pool = pool,
                    selectedTagIds = selectedTagIds,
                    onToggleTag = onToggleTag,
                )
                poolFooter()
            }
            feedback()
            SheetAddTagRow(
                draft = draft,
                onDraftChange = { draft = it },
                onCreateTag = onCreateTag,
            )
            SheetFooterButtons(
                savingTags = savingTags,
                saveEnabled = saveEnabled,
                onSave = onSave,
                onDismiss = onDismiss,
            )
        }
    }
}

@Composable
private fun SheetCurrentTags(
    pool: List<String>,
    selectedTagIds: List<String>,
    onUnbindTag: (String) -> Unit,
) {
    SheetTagSection(title = stringResource(R.string.detail_tag_section_current)) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .height(TAG_CURRENT_TAGS_HEIGHT)
                .verticalScroll(rememberScrollState()),
        ) {
            val selectedChips = pool.filter { it in selectedTagIds }
            if (selectedChips.isEmpty()) {
                SheetEmptyHint()
            } else {
                FlowRow(
                    horizontalArrangement = Arrangement.spacedBy(QimengDimens.SpaceS),
                    verticalArrangement = Arrangement.spacedBy(QimengDimens.SpaceXXS),
                ) {
                    selectedChips.forEach { chip ->
                        DisplayPill(
                            text = chip,
                            selected = true,
                            trailing = {
                                Icon(
                                    imageVector = ClearIcon,
                                    contentDescription = stringResource(R.string.detail_tag_remove_selection),
                                    tint = MaterialTheme.colorScheme.onPrimary,
                                    modifier = Modifier
                                        .padding(start = QimengDimens.SpaceS)
                                        .size(TAG_CLEAR_ICON_SIZE)
                                        .clickable { onUnbindTag(chip) },
                                )
                            },
                        )
                    }
                }
            }
        }
    }
}

@Composable
private fun SheetTagPool(
    pool: List<String>,
    selectedTagIds: List<String>,
    onToggleTag: (String) -> Unit,
) {
    if (pool.isEmpty()) {
        return
    }
    SheetTagSection(title = stringResource(R.string.detail_tag_section_pool)) {
        FlowRow(
            horizontalArrangement = Arrangement.spacedBy(QimengDimens.SpaceS),
            verticalArrangement = Arrangement.spacedBy(QimengDimens.SpaceS),
        ) {
            pool.forEach { chip ->
                key(chip) {
                    val selected = chip in selectedTagIds
                    val background by animateColorAsState(
                        targetValue = if (selected) MaterialTheme.colorScheme.primaryContainer
                            else MaterialTheme.colorScheme.surfaceContainerLow,
                        animationSpec = tween(TAG_SELECTION_TRANSITION_MS),
                        label = "tagBackground",
                    )
                    val foreground by animateColorAsState(
                        targetValue = if (selected) MaterialTheme.colorScheme.onPrimaryContainer
                            else MaterialTheme.colorScheme.onSurface,
                        animationSpec = tween(TAG_SELECTION_TRANSITION_MS),
                        label = "tagForeground",
                    )
                    Surface(
                        shape = RoundedCornerShape(QimengDimens.RankCardCornerRadius),
                        color = background,
                        border = BorderStroke(
                            QimengDimens.DividerThickness,
                            if (selected) MaterialTheme.colorScheme.primary
                            else MaterialTheme.colorScheme.outlineVariant,
                        ),
                    ) {
                        Row(
                            verticalAlignment = Alignment.CenterVertically,
                            modifier = Modifier
                                .heightIn(min = TAG_POOL_CHIP_HEIGHT)
                                .toggleable(
                                    value = selected,
                                    role = Role.Checkbox,
                                    onValueChange = { onToggleTag(chip) },
                                )
                                .padding(
                                    horizontal = QimengDimens.SpaceL,
                                    vertical = QimengDimens.SpaceS,
                                ),
                        ) {
                            Text(
                                text = chip,
                                style = MaterialTheme.typography.labelLarge.copy(
                                    fontSize = TAG_POOL_CHIP_TEXT_SIZE,
                                    fontWeight = FontWeight.Medium,
                                ),
                                color = foreground,
                            )
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun SheetAddTagRow(
    draft: String,
    onDraftChange: (String) -> Unit,
    onCreateTag: (name: String, onCreated: () -> Unit) -> Unit,
) {
    val addDesc = stringResource(R.string.detail_tag_add_desc)
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(top = TAG_ADD_ROW_TOP_SPACING),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(TAG_ADD_ROW_SPACING),
    ) {
        QimengCapsuleTextField(
            value = draft,
            onValueChange = onDraftChange,
            placeholder = stringResource(R.string.detail_tag_new_placeholder),
            singleLine = true,
            modifier = Modifier.weight(1f).testTag("detail-new-tag"),
        )
        Surface(
            onClick = { onCreateTag(draft) { onDraftChange("") } },
            enabled = draft.isNotBlank(),
            shape = RoundedCornerShape(QimengDimens.PillCornerRadius),
            color = MaterialTheme.colorScheme.primary,
            modifier = Modifier
                .width(TAG_ADD_BUTTON_WIDTH)
                .height(QimengDimens.CapsuleFieldHeight)
                .semantics { contentDescription = addDesc },
        ) {
            Box(contentAlignment = Alignment.Center) {
                Text(
                    text = stringResource(R.string.detail_tag_create),
                    style = MaterialTheme.typography.labelLarge,
                    color = MaterialTheme.colorScheme.onPrimary,
                )
            }
        }
    }
}

@OptIn(ExperimentalMaterial3ExpressiveApi::class)
@Composable
private fun SheetFooterButtons(
    savingTags: Boolean,
    saveEnabled: Boolean,
    onSave: () -> Unit,
    onDismiss: () -> Unit,
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(top = TAG_FOOTER_TOP_SPACING),
        horizontalArrangement = Arrangement.End,
        verticalAlignment = Alignment.CenterVertically,
    ) {
        TextButton(onClick = onDismiss, enabled = !savingTags) {
            Text(text = stringResource(R.string.detail_cancel))
        }
        Spacer(modifier = Modifier.width(QimengDimens.SpaceS))
        Button(onClick = onSave, enabled = !savingTags && saveEnabled, colors = qimengFilledButtonColors()) {
            if (savingTags) {
                LoadingIndicator(modifier = Modifier.size(TAG_SAVE_PROGRESS_SIZE))
                Spacer(modifier = Modifier.width(QimengDimens.SpaceS))
            }
            Text(text = stringResource(R.string.detail_save))
        }
    }
}

@Composable
private fun SheetTagSection(title: String, content: @Composable () -> Unit) {
    Column {
        Text(
            text = title,
            style = MaterialTheme.typography.titleSmall.copy(
                fontSize = TAG_SECTION_TITLE_TEXT_SIZE,
                fontWeight = FontWeight.Normal,
            ),
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.padding(vertical = QimengDimens.SpaceS),
        )
        content()
    }
}

@Composable
private fun SheetEmptyHint() {
    Text(
        text = stringResource(R.string.detail_tag_empty),
        style = MaterialTheme.typography.titleSmall.copy(
            fontSize = TAG_SECTION_TITLE_TEXT_SIZE,
            fontWeight = FontWeight.Normal,
        ),
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        modifier = Modifier.padding(top = QimengDimens.SpaceS),
    )
}
