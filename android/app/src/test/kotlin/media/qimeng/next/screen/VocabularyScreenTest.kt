package media.qimeng.next.screen

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class VocabularyScreenTest {
    @Test fun wholeGroupEditorIsOfferedForSourceAndCharacterVocabulary() {
        assertTrue(supportsWholeGroupEditing("sources"))
        assertTrue(supportsWholeGroupEditing("characters"))
    }

    @Test fun wholeGroupEditorStaysOutOfTheTagVocabulary() {
        assertFalse(supportsWholeGroupEditing("tags"))
        assertFalse(supportsWholeGroupEditing(""))
    }
}
