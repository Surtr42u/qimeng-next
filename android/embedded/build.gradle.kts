plugins { alias(libs.plugins.android.library) }
android {
    namespace = "media.qimeng.next.embedded"
    compileSdk = 37
    defaultConfig { minSdk = 26; testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner" }
    ndkVersion = "28.2.13676358"
    compileOptions { sourceCompatibility = JavaVersion.VERSION_17; targetCompatibility = JavaVersion.VERSION_17 }
    packaging { jniLibs { useLegacyPackaging = true; keepDebugSymbols += setOf("**/libqimeng*.so", "**/libffmpeg_cli.so", "**/libffprobe_cli.so") } }
}
dependencies {
    api(project(":runtime-api"))
    implementation(project(":core:transport"))
    implementation(libs.core)
    implementation(libs.coroutines)
    implementation(libs.work)
    testImplementation(libs.junit)
    androidTestImplementation("androidx.test.ext:junit:1.2.1")
    androidTestImplementation("androidx.test:runner:1.6.2")
}
