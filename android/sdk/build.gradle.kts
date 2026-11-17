plugins { alias(libs.plugins.kotlin.jvm) }
kotlin {
    jvmToolchain(17)
    sourceSets.main { kotlin.srcDir("../../api/generated/kotlin/src/main/kotlin") }
}
dependencies {
    api(libs.retrofit)
    api(libs.retrofit.gson)
    api(libs.okhttp)
    api(libs.gson)
    implementation("org.jetbrains.kotlinx:kotlinx-coroutines-core:1.10.2")
}
