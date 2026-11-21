plugins { alias(libs.plugins.android.application); alias(libs.plugins.kotlin.compose) }
android {
    namespace = "media.qimeng.next"
    compileSdk = 37
    defaultConfig {
        applicationId = "media.qimeng.next"
        minSdk = 26
        targetSdk = 35
        versionCode = 1
        versionName = "0.1.0-dev"
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
    }
    buildFeatures { compose = true; buildConfig = true }
    buildTypes {
        // 交付/装机包必须是非 debuggable 的 release（旧版装机包同样是 release：minify + debug 签名）。
        // 原因有两条，都在工具链里可查：
        // 1) AGP 只把 assets/dexopt/baseline.prof 打进非 debuggable 变体
        //    （PackageAndroidArtifact$CreationAction 对 getDebuggable()==true 不接 BINARY_ART_PROFILE 制品）；
        // 2) ART 对 debuggable 包固定用 verify 过滤安装，不会按 profile 做 AOT，JIT 也要等跑热。
        // 因此 debug 包只用于开发/仪器测试；性能对比与设备使用一律装 release（debug 签名，可覆盖安装）。
        release {
            isMinifyEnabled = true
            isShrinkResources = true
            // 本地实测用非发布签名：沿用 debug 签名档，保证能直接覆盖安装到已装 debug 的设备。
            signingConfig = signingConfigs.getByName("debug")
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
        }
    }
    flavorDimensions += "runtime"
    productFlavors {
        create("client") { dimension = "runtime" }
        if (providers.gradleProperty("qimeng.embedded").orNull != "false") create("embedded") { dimension = "runtime" }
    }
    compileOptions { sourceCompatibility = JavaVersion.VERSION_17; targetCompatibility = JavaVersion.VERSION_17 }
    packaging {
        resources.excludes += "/META-INF/{AL2.0,LGPL2.1}"
        jniLibs { useLegacyPackaging = true; keepDebugSymbols += setOf("**/libqimeng*.so", "**/libffmpeg_cli.so", "**/libffprobe_cli.so") }
    }
}
androidComponents.onVariants { variant ->
    variant.deviceTests.values.forEach { test ->
        test.sources.kotlin?.addStaticSourceDirectory(rootProject.file("test-support").absolutePath)
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
    // Baseline Profile 安装器（API 24+ 首次启动把 assets/dexopt/baseline.prof 写入 ART 的 cur 目录）。
    // 之前只靠 Compose 的传递依赖带进来：一旦依赖树变化，profile 就没人装了，故显式声明。
    implementation(libs.profileinstaller)
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
    androidTestImplementation(platform(libs.compose.bom))
    androidTestImplementation(libs.compose.test)
    androidTestImplementation("androidx.test.ext:junit:1.2.1")
    androidTestImplementation("androidx.test:runner:1.6.2")
    debugImplementation(libs.compose.test.manifest)
}
