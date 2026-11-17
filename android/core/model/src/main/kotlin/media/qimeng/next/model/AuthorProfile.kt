package media.qimeng.next.model

data class ProfileAccount(val platform: String, val identifier: String, val url: String, val publishingUrl: String)
data class ProfileValues(val handle: String? = null, val displayName: String? = null,
    val biographyOriginal: String? = null, val biographyZh: String? = null, val avatarSourceUrl: String? = null,
    val summary: String? = null, val note: String? = null, val accounts: List<ProfileAccount>? = null,
    val profileUrl: String? = null, val publicName: String? = null, val remark: String? = null, val identityStatus: String? = null)
data class ProfileEvidence(val url: String, val title: String, val fields: List<String>, val status: String, val note: String, val checkedAt: String?)
data class ProfileAvatar(val url: String, val cacheKey: String, val origin: String, val sourceUrl: String, val fetchedAt: String)
data class ProfileBlock(val block: String, val revision: Long, val values: ProfileValues, val evidence: List<ProfileEvidence>,
    val source: String, val updatedAt: String?, val avatar: ProfileAvatar?)
data class ProfileDocument(val target: Session, val authorId: String, val editable: Boolean, val blocks: List<ProfileBlock>)
data class ProfileSummary(val avatar: ProfileAvatar?, val biographyOriginal: String, val biographyZh: String)
data class ProfileCandidate(val id: String, val block: String, val baseRevision: Long, val state: String,
    val values: ProfileValues, val evidence: List<ProfileEvidence>, val createdAt: String)
data class ProfileArchive(val id: String, val authorId: String, val authorName: String, val reason: String,
    val createdAt: String, val blockCount: Int, val candidateCount: Int, val conflicts: List<String>)
