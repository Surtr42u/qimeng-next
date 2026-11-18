package media.qimeng.next.ui.component

/** Presentation choices only. Date/range interpretation and matching belong to the service. */
enum class FilterSort { DEFAULT, VIEW_COUNT, SIZE_BYTES }
enum class FilterOrder { DESC, ASC }
enum class PanelCountRange { ALL, NONE, LOW, MID, HIGH }
enum class PanelSizeRange { ALL, LT_1M, M_1_TO_10, M_10_TO_50, GT_50M }
enum class PanelDateRange { ALL, TODAY, WEEK, MONTH, QUARTER, YEAR, YEAR_RANGE }
enum class PanelTagMode { FUZZY, EXACT }
const val PANEL_DEFAULT_YEAR_FROM = 2020
const val PANEL_MIN_YEAR = 1990

data class FilterPanelDraft(val sort: FilterSort = FilterSort.DEFAULT, val order: FilterOrder = FilterOrder.DESC,
    val viewRange: PanelCountRange = PanelCountRange.ALL, val playRange: PanelCountRange = PanelCountRange.ALL,
    val sizeRange: PanelSizeRange = PanelSizeRange.ALL, val dateRange: PanelDateRange = PanelDateRange.ALL,
    val tagMode: PanelTagMode = PanelTagMode.FUZZY, val tagIds: List<String> = emptyList(), val yearFrom: Int? = null, val yearTo: Int? = null) : java.io.Serializable

/** The object captured by a delete confirmation retains the revision that the owner actually saw. */
data class FilterTag(val id: String, val name: String, val revision: Long)
