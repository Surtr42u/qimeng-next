plugins { alias(libs.plugins.kotlin.jvm) }
kotlin { jvmToolchain(17) }
dependencies { api("org.jetbrains.kotlinx:kotlinx-coroutines-core:1.10.2") }
