package media.qimeng.next.model

data class AIResearchSources(val enabled: Boolean, val providers: List<String>)

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
    val researchProviders: List<String> = emptyList(),
    val canLookupImages: Boolean = false,
)

fun canGrantImageLookup(
    canResearch: Boolean,
    maxResearchCalls: Int,
    libraryIds: List<String>,
    canIngest: Boolean,
    canResearchAssets: Boolean,
): Boolean = canResearch && maxResearchCalls > 0 && libraryIds.isNotEmpty() && (canIngest || canResearchAssets)

data class AIAccessGrant(
    val id: String,
    val state: String,
    val createdAt: String,
    val expiresAt: String,
    val policy: AIAccessPolicy,
) {
    val canLookupImages: Boolean get() = policy.canLookupImages
}
