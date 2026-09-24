import java.util.Properties

plugins {
    id("com.android.application")
    id("com.google.gms.google-services")
    id("io.sentry.android.gradle") version "6.14.0"
    id("org.jetbrains.kotlin.plugin.compose")
}

// Release signing credentials. They live outside git, in one of two places:
// keystore.properties beside this module (see .gitignore) on a machine that
// keeps the password on disk, or the HW_SMS_* environment variables on one
// that keeps it in a password manager. The file wins where both exist.
val keystoreProps = Properties().apply {
    val f = rootProject.file("keystore.properties")
    if (f.exists()) f.inputStream().use { load(it) }
}

fun signingProp(key: String, env: String): String? =
    keystoreProps.getProperty(key) ?: System.getenv(env)

// App version, hand-bumped in version.properties. Kept out of this file so the
// number sits somewhere obvious rather than buried in a build script, and so a
// version bump is a one-line diff that reviews cleanly.
val appVersion = Properties().apply {
    rootProject.file("version.properties").inputStream().use { load(it) }
}.getProperty("versionCode").trim().toInt()

android {
    compileSdk = 37

    defaultConfig {
        // CUSTOM: was "com.httpsms" (upstream's). This MUST equal a
        // package_name in app/google-services.json or the google-services gradle
        // plugin fails the build with "No matching client found for package
        // name". The Firebase app registered for this deployment is
        // nl.hollandworx.sms.
        //
        // Only applicationId changes. `namespace` below stays com.httpsms: that
        // one is the package for the generated R and BuildConfig classes and has
        // to match the Kotlin source tree (com/httpsms/...), which Firebase does
        // not care about. Android supports the two differing.
        applicationId = "nl.hollandworx.sms"
        minSdk = 28
        targetSdk = 37
        // One number for both. Play permanently rejects a re-used or lower
        // versionCode, so bump version.properties before every upload.
        versionCode = appVersion
        versionName = appVersion.toString()
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
    }

    // WHY THIS TOLERATES MISSING CREDENTIALS. A checkout without the keystore
    // (a fresh clone, CI running unit tests) still has to configure. So an
    // absent storeFile leaves the config half-built here and unreferenced
    // below, and assembleRelease then emits an UNSIGNED apk exactly as it did
    // before this block existed — the case scripts/build-release.sh refuses
    // for --release and --bundle. Failing the build instead would break `test`
    // on every machine that has no business holding the signing key.
    signingConfigs {
        create("release") {
            val store = signingProp("storeFile", "HW_SMS_STORE_FILE")
            if (store != null) {
                storeFile = file(store)
                storePassword = signingProp("storePassword", "HW_SMS_STORE_PASSWORD")
                keyAlias = signingProp("keyAlias", "HW_SMS_KEY_ALIAS")
                keyPassword = signingProp("keyPassword", "HW_SMS_KEY_PASSWORD")
            }
        }
    }

    buildTypes {
        getByName("debug") {
            manifestPlaceholders["sentryEnvironment"] = "development"
        }
        getByName("release") {
            manifestPlaceholders["sentryEnvironment"] = "production"
            // null when no credentials were found, which is what makes the
            // unsigned-but-still-configurable case above work.
            signingConfig = signingConfigs.getByName("release").takeIf { it.storeFile != null }
            isMinifyEnabled = false
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
        }
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_1_8
        targetCompatibility = JavaVersion.VERSION_1_8
    }
    namespace = "com.httpsms"

    buildFeatures {
        buildConfig = true
        compose = true
    }
}

dependencies {
    val composeBom = platform("androidx.compose:compose-bom:2026.06.01")
    implementation(composeBom)
    androidTestImplementation(composeBom)

    implementation("androidx.compose.ui:ui")
    implementation("androidx.compose.ui:ui-graphics")
    implementation("androidx.compose.ui:ui-tooling-preview")
    implementation("androidx.compose.material3:material3")
    implementation("androidx.compose.material:material-icons-extended")
    implementation("androidx.activity:activity-compose:1.13.0")
    implementation("androidx.lifecycle:lifecycle-viewmodel-compose:2.11.0")

    implementation(platform("com.google.firebase:firebase-bom:34.16.0"))
    implementation("com.journeyapps:zxing-android-embedded:4.3.0")
    implementation("com.google.firebase:firebase-analytics")
    implementation("com.google.firebase:firebase-messaging")
    implementation("com.squareup.okhttp3:okhttp:5.4.0")
    implementation("com.jakewharton.timber:timber:5.0.1")
    implementation("androidx.preference:preference-ktx:1.2.1")
    implementation("androidx.work:work-runtime-ktx:2.11.2")
    implementation("androidx.core:core-ktx:1.19.0")
    implementation("androidx.cardview:cardview:1.0.0")
    implementation("com.beust:klaxon:5.6")
    implementation("androidx.appcompat:appcompat:1.7.1")
    implementation("org.apache.commons:commons-text:1.15.0")
    implementation("com.google.android.material:material:1.14.0")
    implementation("androidx.constraintlayout:constraintlayout:2.2.1")
    implementation("com.googlecode.libphonenumber:libphonenumber:9.0.34")
    implementation("com.klinkerapps:android-smsmms:5.2.6")
    testImplementation("junit:junit:4.13.2")
    androidTestImplementation("androidx.test.ext:junit:1.3.0")
    androidTestImplementation("androidx.test.espresso:espresso-core:3.7.0")
}
