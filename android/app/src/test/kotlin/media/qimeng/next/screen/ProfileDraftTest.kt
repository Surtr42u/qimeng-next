package media.qimeng.next.screen

import media.qimeng.next.model.*
import org.junit.Assert.*
import org.junit.Test

class ProfileDraftTest {
    @Test fun partialCandidatePreservesUnspecifiedFieldsButExplicitEmptyClears() {
        val block = block("basic", ProfileValues(publicName = "Existing name", summary = "Existing summary", note = "Private note"))
        val draft = profileDraftValues(block, candidate("basic", ProfileValues(summary = "", publicName = "Candidate name")))
        assertEquals("Candidate name", draft.publicName)
        assertEquals("", draft.summary)
        assertEquals("Private note", draft.note)
        assertNull(draft.handle)
    }

    @Test fun changedOriginalDoesNotReuseUnreviewedExistingTranslation() {
        val block = block("x", ProfileValues(handle = "stable", biographyOriginal = "Old original", biographyZh = "Old translation"))
        val changed = profileDraftValues(block, candidate("x", ProfileValues(biographyOriginal = "New original")))
        assertEquals("", changed.biographyZh)
        assertEquals("stable", changed.handle)
        val unchanged = profileDraftValues(block, candidate("x", ProfileValues(displayName = "New display name")))
        assertEquals("Old translation", unchanged.biographyZh)
    }

    @Test fun emptyAccountsCandidateClearsWithoutRestoringCurrentAccounts() {
        val block = block("accounts", ProfileValues(accounts = listOf(ProfileAccount("site", "author", "", ""))))
        assertTrue(profileDraftValues(block, candidate("accounts", ProfileValues(accounts = emptyList()))).accounts!!.isEmpty())
        assertEquals(1, profileDraftValues(block, candidate("accounts", ProfileValues())).accounts!!.size)
    }

    private fun block(name: String, values: ProfileValues) = ProfileBlock(name, 3, values, emptyList(), "human", null, null)
    private fun candidate(name: String, values: ProfileValues) = ProfileCandidate("candidate", name, 3, "pending", values, emptyList(), "synthetic")
}
