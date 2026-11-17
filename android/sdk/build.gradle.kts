plugins { `java-library`; alias(libs.plugins.kotlin.jvm) }
kotlin {
    jvmToolchain(17)
    sourceSets.main { kotlin.srcDir("../../api/generated/kotlin/src/main/kotlin") }
}
dependencies {
    api(libs.retrofit)
    api(libs.retrofit.gson)
    api(libs.okhttp)
    api(libs.gson)
    implementation("com.squareup.retrofit2:converter-scalars:2.11.0")
    implementation("com.squareup.okhttp3:logging-interceptor:4.12.0")
    implementation("org.jetbrains.kotlinx:kotlinx-coroutines-core:1.10.2")
}
