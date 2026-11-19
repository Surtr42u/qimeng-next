plugins { alias(libs.plugins.android.application); alias(libs.plugins.kotlin.compose) }
val migrationTest = providers.gradleProperty("qimeng.migrationTest").orNull
android {
    namespace = "media.qimeng.next"
    compileSdk = 37
    defaultConfig {
        applicationId = "media.qimeng.next"
        minSdk = 26
        targetSdk = 35
        versionCode = 1
        versionName = "0.1.0-dev"
        testInstrumentationRunner = when (migrationTest) {
            null -> "androidx.test.runner.AndroidJUnitRunner"
            "acceptance" -> "media.qimeng.next.test.MigrationAcceptance"
            "cloudFixture" -> "media.qimeng.next.cloudfixture.CloudFixture"
            else -> error("Unknown migration test source")
        }
        if (migrationTest == "cloudFixture") testApplicationId = "media.qimeng.next.cloudfixture"
    }
    buildFeatures { compose = true; buildConfig = true }
    flavorDimensions += "runtime"
    productFlavors {
        create("client") { dimension = "runtime" }
        if (providers.gradleProperty("qimeng.embedded").orNull != "false") create("embedded") { dimension = "runtime" }
    }
    if (migrationTest != null) {
        sourceSets.configureEach {
            if (name == "androidTest") setRoot("src/$migrationTest")
            if (name == "androidTestEmbedded") setRoot("src/noAdditionalMigrationTests")
        }
    }
    compileOptions { sourceCompatibility = JavaVersion.VERSION_17; targetCompatibility = JavaVersion.VERSION_17 }
    packaging {
        resources.excludes += "/META-INF/{AL2.0,LGPL2.1}"
        jniLibs { useLegacyPackaging = true; keepDebugSymbols += setOf("**/libqimeng*.so", "**/libffmpeg_cli.so", "**/libffprobe_cli.so") }
    }
}
androidComponents.onVariants { variant ->
    variant.deviceTests.values.forEach { test ->
        val sources = if (migrationTest == null) rootProject.file("test-support") else project.file("src/$migrationTest/kotlin")
        test.sources.kotlin?.addStaticSourceDirectory(sources.absolutePath)
    }
}
dependencies {
    implementation(project(":runtime-api"))
    if (providers.gradleProperty("qimeng.embedded").orNull != "false") add("embeddedImplementation", project(":embedded"))
    implementation(project(":core:model"))
    implementation(project(":core:data"))
    implementation(project(":core:ui"))
    implementation(libs.core)
    implementation(libs.activity)
    implementation(libs.work)
    implementation(libs.lifecycle.runtime)
    implementation(libs.lifecycle.viewmodel)
    implementation(libs.coil)
    implementation(libs.coil.network)
    implementation(libs.coil.gif)
    implementation(libs.media3.player)
    implementation(libs.media3.ui)
    implementation(libs.media3.okhttp)
    debugImplementation(libs.compose.tooling)
    testImplementation(libs.junit)
    testImplementation(libs.coroutines.test)
    if (migrationTest == null) {
        androidTestImplementation(platform(libs.compose.bom))
        androidTestImplementation(libs.compose.test)
        androidTestImplementation("androidx.test.ext:junit:1.2.1")
        androidTestImplementation("androidx.test:runner:1.6.2")
    }
    debugImplementation(libs.compose.test.manifest)
}
