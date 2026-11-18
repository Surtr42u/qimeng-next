package media.qimeng.next.model

data class AIAccessPolicy(
    val label: String,
    val libraryIds: List<String>,
    val authorIds: List<String>,
    val allAuthors: Boolean,
    val allowedFields: List<String>,
    val candidateBlocks: List<String>,
    val allowClear: Boolean,
    val canIngest: Boolean,
    val canResearch: Boolean,
    val canOrganize: Boolean,
    val maxResearchCalls: Int,
    val maxUpdates: Int,
    val maxTasksPerDay: Int,
    val validDays: Int,
    val maxCallsPerTask: Int,
    val taskSeconds: Int,
    val canRecycleSources: Boolean = false,
    val canResearchAssets: Boolean = false,
    val assetFields: List<String> = emptyList(),
)
data class AIAccessGrant(val id: String, val label: String, val state: String, val expiresAt: String,
    val libraryIds: List<String>, val authorIds: List<String>, val allAuthors: Boolean, val allowedFields: List<String>,
    val candidateBlocks: List<String>, val canIngest: Boolean, val canResearch: Boolean, val canOrganize: Boolean,
    val maxTasksPerDay: Int, val maxUpdates: Int, val maxResearchCalls: Int, val canRecycleSources: Boolean = false,
    val canResearchAssets: Boolean = false, val assetFields: List<String> = emptyList())
