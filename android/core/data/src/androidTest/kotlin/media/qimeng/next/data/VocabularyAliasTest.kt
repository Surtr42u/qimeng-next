package media.qimeng.next.data

import java.util.UUID
import kotlinx.coroutines.runBlocking
import media.qimeng.next.sdk.model.AliasSetUpdate
import org.junit.Assert.*
import org.junit.Test

class VocabularyAliasTest {
    @Test fun lookupByAliasKeepsTheSameTagAcrossRenameAndClear() = runBlocking {
        val client = signInToCloud()
        val suffix = UUID.randomUUID().toString()
        val name = "云端词条-$suffix"
        val alias = "旧称-$suffix"
        try {
            client.media.createTag(name)
            val first = client.management.vocabulary("tags", null, name).items.single()
            val saved = client.api.setTagAliases(first.id, "alias-set-$suffix", AliasSetUpdate(first.revision, listOf(alias))).bodyOrThrow()
            val matched = client.management.vocabulary("tags", null, alias).items.single()
            assertEquals(saved.id, matched.id)
            assertEquals(listOf(alias), matched.aliases)

            val newName = "改名-$suffix"
            client.management.saveVocabulary("tags", matched, newName, matched.aliases, null)
            val renamed = client.management.vocabulary("tags", null, alias).items.single()
            assertEquals(first.id, renamed.id)
            assertEquals(newName, renamed.name)
            client.api.setTagAliases(renamed.id, "alias-clear-$suffix", AliasSetUpdate(renamed.revision, emptyList())).bodyOrThrow()
            assertTrue(client.management.vocabulary("tags", null, alias).items.isEmpty())
            assertEquals(first.id, client.management.vocabulary("tags", null, newName).items.single().id)
        } finally {
            client.media.logout()
        }
    }
}
