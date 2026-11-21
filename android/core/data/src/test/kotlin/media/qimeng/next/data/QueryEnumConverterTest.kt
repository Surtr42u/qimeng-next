package media.qimeng.next.data

import com.google.gson.annotations.SerializedName
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Test
import retrofit2.Converter
import retrofit2.Retrofit
import retrofit2.http.GET
import retrofit2.http.Query

/**
 * Release builds rename enum constant fields, so the wire value must come from the constant
 * instance and its @SerializedName annotation. Looking the field up with `getField(value.name)`
 * throws NoSuchFieldException in an R8 build and every list request dies before it is sent.
 */
class QueryEnumConverterTest {
    private enum class Period {
        @SerializedName("7d") week,
        @SerializedName("all") everything,
        raw;

        override fun toString(): String = "toString must not leak into the query"
    }

    private interface Sample {
        @GET("assets")
        fun page(@Query("period") period: Period, @Query("plain") plain: String)
    }

    private val retrofit = Retrofit.Builder().baseUrl("http://127.0.0.1/").build()

    private fun queryAnnotations(): Array<Annotation> =
        Sample::class.java.declaredMethods.single { it.name == "page" }.parameterAnnotations[0]

    @Suppress("UNCHECKED_CAST")
    private fun converterFor(type: Class<*>): Converter<Period, String>? =
        QueryEnumConverterFactory.stringConverter(type, queryAnnotations(), retrofit) as Converter<Period, String>?

    @Test fun wireValuesComeFromSerializedNameNotNameOrToString() {
        val converter = converterFor(Period::class.java)
        assertNotNull(converter)
        assertEquals("7d", converter!!.convert(Period.week))
        assertEquals("all", converter.convert(Period.everything))
    }

    @Test fun constantsWithoutSerializedNameFallBackToTheirName() {
        val converter = converterFor(Period::class.java)
        assertEquals("raw", converter!!.convert(Period.raw))
    }

    @Test fun nonEnumQueryParametersAreLeftToGson() {
        assertNull(converterFor(String::class.java))
    }
}
