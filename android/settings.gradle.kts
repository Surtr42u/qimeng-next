pluginManagement {
    repositories { google(); mavenCentral(); gradlePluginPortal() }
}
dependencyResolutionManagement {
    repositoriesMode.set(RepositoriesMode.FAIL_ON_PROJECT_REPOS)
    repositories { google(); mavenCentral() }
}
rootProject.name = "QimengNext"
include(":app", ":core:model", ":core:data", ":core:ui", ":core:transport", ":runtime-api", ":sdk")
if (providers.gradleProperty("qimeng.embedded").orNull != "false") include(":embedded")
