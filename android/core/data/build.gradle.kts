plugins { alias(libs.plugins.android.library) }
android {
    namespace = "media.qimeng.next.data"
    compileSdk = 37
    defaultConfig { minSdk = 26; testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner" }
    compileOptions { sourceCompatibility = JavaVersion.VERSION_17; targetCompatibility = JavaVersion.VERSION_17 }
}
dependencies {
    implementation(project(":core:transport"))
    api(project(":core:model"))
    implementation(project(":sdk"))
    implementation(libs.core)
    implementation(libs.work)
    api(libs.coroutines)
    implementation(libs.gson)
    api(libs.coil.core)
    api(libs.okhttp)
    implementation(libs.retrofit.gson)
    testImplementation(libs.junit)
    testImplementation(libs.coroutines.test)
    androidTestImplementation("androidx.test.ext:junit:1.2.1")
    androidTestImplementation("androidx.test:runner:1.6.2")
    androidTestImplementation("com.squareup.okhttp3:mockwebserver:4.12.0")
    androidTestImplementation("com.squareup.okhttp3:okhttp-tls:4.12.0")
}
