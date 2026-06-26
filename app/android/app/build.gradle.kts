import java.util.Properties
import org.gradle.api.artifacts.VersionCatalogsExtension

plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.compose)
}

// --- build provenance: read git at configure time ---
// Uses providers.exec (configuration-cache safe); falls back to "" if git isn't available.
fun git(vararg args: String): String = try {
    providers.exec {
        commandLine("git", *args)
        isIgnoreExitValue = true
    }.standardOutput.asText.get().trim()
} catch (e: Exception) { "" }

val gitCommit = git("rev-parse", "HEAD").ifEmpty { "unknown" }
val gitCommitShort = git("rev-parse", "--short", "HEAD").ifEmpty { "unknown" }
val gitTreeClean = git("status", "--porcelain").isEmpty()
// DETERMINISTIC build time: the commit's committer date (ISO-8601 UTC), NOT wall-clock. A wall-clock
// time would change the APK bytes on every build and break reproducibility. Same commit -> same time.
val buildTimeUtc: String = git("log", "-1", "--pretty=%cI", "HEAD").ifEmpty { "unknown" }
val manifestRoot = rootProject.file("MANIFEST.root")
    .let { if (it.exists()) it.readText().trim() else "" }

// Build-environment provenance. Two tiers:
//  - STABLE values go into BuildConfig (below): they're the same on any machine that uses the
//    pinned toolchain, so they don't change the APK bytes. Major JVM (e.g. "21"), OS name,
//    and the tool versions pinned in libs.versions.toml.
//  - FULL values (exact JDK patch, kernel string) go into ghost/build-env.txt via the
//    writeBuildEnv task, NOT into the APK, so reproducibility is preserved.
val jvmMajor: String = System.getProperty("java.version").orEmpty().substringBefore(".")
val osName: String = System.getProperty("os.name").orEmpty()
val gradleVersion: String = gradle.gradleVersion
// Read AGP/Kotlin versions straight from the version catalog [versions] table.
val catalog = extensions.getByType<VersionCatalogsExtension>().named("libs")
val agpVersion: String = catalog.findVersion("agp").map { it.toString() }.orElse("unknown")
val kotlinVersion: String = catalog.findVersion("kotlin").map { it.toString() }.orElse("unknown")

// Read the llama.cpp pins from CMakeLists.txt. The regex anchors on `set(LLAMA_CPP_...` so it matches
// the actual set() calls, NOT the comment line above them that mentions both identifiers. Matching
// the bare identifier would let `.find()` latch onto the comment and run forward to the wrong quote
// (which is how the commit field once came out as the tag value).
fun cmakePin(key: String): String {
    val cmake = rootProject.file("app/src/main/cpp/CMakeLists.txt")
    if (!cmake.exists()) return "unknown"
    return Regex("""set\(\s*$key\s+"([^"]+)"""")
        .find(cmake.readText())?.groupValues?.get(1) ?: "unknown"
}
val llamaTag: String = cmakePin("LLAMA_CPP_TAG")
val llamaCommit: String = cmakePin("LLAMA_CPP_COMMIT")

android {
    val localProps = Properties().apply {
        rootProject.file("local.properties").takeIf { it.exists() }
            ?.inputStream()?.use { load(it) }
    }

    namespace = "com.localghost.app"
    compileSdk {
        version = release(37)
    }
    defaultConfig {
        applicationId = "com.localghost.app"
        minSdk = 35
        targetSdk = 36
        versionCode = 1
        versionName = "1.0"
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"

        // Dev convenience only. The PUBLIC release build leaves these EMPTY: the app reads the
        // box URL + device token from its own encrypted storage (written during setup). An empty
        // value at runtime = unconfigured = show setup. This keeps the release APK reproducible
        // (no machine-specific data baked in).
        buildConfigField("String", "NAS_BASE_URL",
            "\"${localProps.getProperty("NAS_BASE_URL", "")}\"")
        buildConfigField("String", "DEVICE_TOKEN",
            "\"${localProps.getProperty("DEVICE_TOKEN", "")}\"")

        // Build provenance — surfaced by the in-app VERIFY BUILD screen.
        buildConfigField("String", "GIT_COMMIT", "\"$gitCommit\"")
        buildConfigField("String", "GIT_COMMIT_SHORT", "\"$gitCommitShort\"")
        buildConfigField("boolean", "GIT_TREE_CLEAN", "$gitTreeClean")
        buildConfigField("String", "BUILD_TIME_UTC", "\"$buildTimeUtc\"")
        buildConfigField("String", "MANIFEST_ROOT", "\"$manifestRoot\"")
        buildConfigField("String", "GITHUB_REPO", "\"https://github.com/localghost-ai/localghost-app\"")
        // Stable build-env (same on any machine using the pinned toolchain; safe in the APK).
        buildConfigField("String", "BUILD_JVM_MAJOR", "\"$jvmMajor\"")
        buildConfigField("String", "BUILD_OS_NAME", "\"$osName\"")
        buildConfigField("String", "BUILD_GRADLE_VERSION", "\"$gradleVersion\"")
        buildConfigField("String", "BUILD_AGP_VERSION", "\"$agpVersion\"")
        buildConfigField("String", "BUILD_KOTLIN_VERSION", "\"$kotlinVersion\"")
    }
    buildTypes {
        release {
            optimization {
                enable = false
            }
        }
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_11
        targetCompatibility = JavaVersion.VERSION_11
    }
    buildFeatures {
        compose = true
        buildConfig = true
    }
    // Keep dependency metadata out of the APK so it doesn't introduce per-build variance.
    dependenciesInfo {
        includeInApk = false
        includeInBundle = false
    }
    buildToolsVersion = "36.0.0"
}
dependencies {
    implementation(platform(libs.androidx.compose.bom))
    implementation(libs.androidx.activity.compose)
    implementation(libs.androidx.compose.material3)
    implementation(libs.androidx.compose.material3.adaptive.navigation.suite)
    implementation(libs.androidx.compose.ui)
    implementation(libs.androidx.compose.ui.graphics)
    implementation(libs.androidx.compose.ui.tooling.preview)
    implementation(libs.androidx.core.ktx)
    implementation(libs.androidx.lifecycle.runtime.ktx)
    implementation(libs.androidx.work.runtime.ktx)
    implementation(libs.androidx.camera.core)
    implementation(libs.androidx.camera.camera2)
    implementation(libs.androidx.camera.lifecycle)
    implementation(libs.androidx.camera.view)
    testImplementation(libs.junit)
    androidTestImplementation(platform(libs.androidx.compose.bom))
    androidTestImplementation(libs.androidx.compose.ui.test.junit4)
    androidTestImplementation(libs.androidx.espresso.core)
    androidTestImplementation(libs.androidx.junit)
    debugImplementation(libs.androidx.compose.ui.test.manifest)
    debugImplementation(libs.androidx.compose.ui.tooling)
}

// Writes the FULL build environment to ghost/build-env.txt so a verifier knows exactly what to
// install to reproduce the build. Deliberately NOT in BuildConfig: the exact JDK patch and kernel
// string vary between machines and would change the APK bytes. Run by tools/release.sh before the
// build; the file is committed and signed alongside the source manifest.
//
// Everything written here is either a toolchain version (build-determining) or clearly-labelled
// context. It deliberately OMITS jdk.home (a machine-specific path, useless for reproduction and a
// minor info leak) and uses the commit's committer time for "Written" (deterministic, so the file
// reproduces). The llama.cpp pins are read from CMakeLists.txt via the anchored regex above.
tasks.register("writeBuildEnv") {
    val out = rootProject.file("ghost/build-env.txt")
    val commit = gitCommit
    val writtenAt = buildTimeUtc
    val jdkVersion = System.getProperty("java.version")
    val jdkVendor = System.getProperty("java.vendor")
    val osVersion = System.getProperty("os.version")
    val osArch = System.getProperty("os.arch")
    val gv = gradle.gradleVersion
    val agp = agpVersion
    val kotlin = kotlinVersion
    val lTag = llamaTag
    val lCommit = llamaCommit
    notCompatibleWithConfigurationCache("writes a provenance file at execution time")
    doLast {
        out.parentFile.mkdirs()
        out.writeText(buildString {
            appendLine("# LocalGhost App Build Environment")
            appendLine("# Build: $commit")
            appendLine("# Written: $writtenAt")
            appendLine("#")
            appendLine("# Build-determining: match these to reproduce the APK bytes.")
            appendLine()
            appendLine("jdk.version      $jdkVersion")
            appendLine("jdk.vendor       $jdkVendor")
            appendLine("gradle.version   $gv")
            appendLine("agp.version      $agp")
            appendLine("kotlin.version   $kotlin")
            appendLine("compileSdk       37")
            appendLine("targetSdk        36")
            appendLine("minSdk           35")
            appendLine("buildTools       36.0.0")
            appendLine("llama.cpp.tag    $lTag")
            appendLine("llama.cpp.commit $lCommit")
            appendLine()
            appendLine("# Informational: NOT build-determining, recorded for context only.")
            appendLine("os.name          ${System.getProperty("os.name")}")
            appendLine("os.version       $osVersion")
            appendLine("os.arch          $osArch")
        })
        println("wrote ${out.relativeTo(rootProject.projectDir)}")
    }
}