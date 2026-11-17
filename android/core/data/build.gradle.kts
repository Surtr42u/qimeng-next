plugins { alias(libs.plugins.android.library); alias(libs.plugins.kotlin.android) }
android {
    namespace = "media.qimeng.next.data"
    compileSdk = 35
    defaultConfig { minSdk = 26 }
    compileOptions { sourceCompatibility = JavaVersion.VERSION_17; targetCompatibility = JavaVersion.VERSION_17 }
}
kotlin { jvmToolchain(17) }
dependencies {
    api(project(":core:model"))
    implementation(project(":sdk"))
    implementation(libs.core)
    implementation(libs.work)
    api(libs.coroutines)
    implementation(libs.gson)
    api(libs.okhttp)
    implementation(libs.retrofit.gson)
    testImplementation(libs.junit)
    testImplementation(libs.coroutines.test)
}
