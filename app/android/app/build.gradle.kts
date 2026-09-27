import java.util.Properties
import org.gradle.api.artifacts.VersionCatalogsExtension
import java.time.Instant
import java.time.ZoneOffset
import java.time.format.DateTimeFormatter

plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.compose)
}

// --- build provenance: read git + source manifest at configure time ---
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
val buildTimeUtc: String = DateTimeFormatter.ISO_INSTANT
    .withZone(ZoneOffset.UTC)
    .format(Instant.now())
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

// THE PHONE'S MODEL (llama.cpp, src/main/cpp). Built only when CMakeLists.txt pins llama.cpp to a
// real 40-character commit: until then the APK builds without it and the app says so (Settings ›
// PHONE MODEL), rather than a placeholder breaking every build. Pin it to the commit the box runs,
// so the phone and the box read the same GGUF the same way:
//     ssh box 'git -C /opt/localghost/llama.cpp rev-parse HEAD'
// The source is fetched at configure time (git + network on the build machine), or taken from a
// local checkout with -PllamaSrc=/path/to/llama.cpp (verified against the pin all the same).
val llamaCmake = file("src/main/cpp/CMakeLists.txt")
val llamaPin: String = if (llamaCmake.exists())
    Regex("""set\(LLAMA_CPP_COMMIT\s+"([0-9a-f]*)"""").find(llamaCmake.readText())?.groupValues?.get(1).orEmpty() else ""
val buildPhoneModel = Regex("^[0-9a-f]{40}$").matches(llamaPin)
val llamaSrc: String = (findProperty("llamaSrc") as String?).orEmpty()
// dotprod+fp16 runs on every arm64 phone that has Android 15; i8mm (Snapdragon 8 Gen 1 and later,
// Tensor G3 and later) is faster for the quantised matmuls: -PllamaArch=armv8.6-a+dotprod+i8mm+fp16
val llamaArch: String = (findProperty("llamaArch") as String?) ?: "armv8.2-a+dotprod+fp16"

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
        // whether this APK carries the phone's model runtime, and which llama.cpp
        buildConfigField("boolean", "HAS_PHONE_MODEL", "$buildPhoneModel")
        buildConfigField("String", "LLAMA_CPP_COMMIT", "\"$llamaPin\"")
        if (buildPhoneModel) {
            ndk { abiFilters += "arm64-v8a" }
            externalNativeBuild {
                cmake {
                    arguments += listOf("-DANDROID_STL=c++_static", "-DLG_CPU_ARCH=$llamaArch") +
                        (if (llamaSrc.isNotEmpty()) listOf("-DFETCHCONTENT_SOURCE_DIR_LLAMA_CPP=$llamaSrc") else emptyList())
                    cppFlags += "-std=c++17"
                }
            }
        }
    }
    if (buildPhoneModel) {
        externalNativeBuild {
            cmake {
                path = llamaCmake
                version = "3.22.1"
            }
        }
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
    implementation("androidx.health.connect:connect-client:1.1.0-alpha07")
    // Media3 , the ONE dependency the video feature earns: a custom DataSource turns the player's
    // byte requests into in-process calls on our authenticated channel, deleting the loopback
    // proxy and its entire attack surface. androidx = the same trust root as Compose.
    implementation("androidx.media3:media3-exoplayer:1.4.1")
    implementation("androidx.media3:media3-ui:1.4.1")
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
tasks.register("writeBuildEnv") {
    val out = rootProject.file("ghost/build-env.txt")
    val commit = gitCommit
    notCompatibleWithConfigurationCache("writes a provenance file at execution time")
    doLast {
        out.parentFile.mkdirs()
        out.writeText(buildString {
            appendLine("# LocalGhost App Build Environment")
            appendLine("# Build: $commit")
            appendLine("# Written: ${DateTimeFormatter.ISO_INSTANT.withZone(ZoneOffset.UTC).format(Instant.now())}")
            appendLine()
            appendLine("jdk.version      ${System.getProperty("java.version")}")
            appendLine("jdk.vendor       ${System.getProperty("java.vendor")}")
            appendLine("jdk.home         ${System.getProperty("java.home")}")
            appendLine("os.name          ${System.getProperty("os.name")}")
            appendLine("os.version       ${System.getProperty("os.version")}")
            appendLine("os.arch          ${System.getProperty("os.arch")}")
            appendLine("gradle.version   ${gradle.gradleVersion}")
            appendLine("agp.version      $agpVersion")
            appendLine("kotlin.version   $kotlinVersion")
            appendLine("compileSdk       37")
            appendLine("targetSdk        36")
            appendLine("minSdk           35")
            appendLine("buildTools       36.0.0")
            val cmake = rootProject.file("app/src/main/cpp/CMakeLists.txt")
            if (cmake.exists()) {
                val txt = cmake.readText()
                val tag = Regex("""LLAMA_CPP_TAG[^"]*"([^"]+)"""").find(txt)?.groupValues?.get(1) ?: "unknown"
                val commit = Regex("""LLAMA_CPP_COMMIT[^"]*"([^"]+)"""").find(txt)?.groupValues?.get(1) ?: "unknown"
                appendLine("llama.cpp.tag    $tag")
                appendLine("llama.cpp.commit $commit")
            }
        })
        println("wrote ${out.relativeTo(rootProject.projectDir)}")
    }
}
