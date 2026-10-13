plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
}

android {
    namespace = "com.wx.sniffer"
    compileSdk = 34

    defaultConfig {
        applicationId = "com.wx.sniffer"
        minSdk = 26
        targetSdk = 34
        versionCode = 3
        versionName = "1.0.3"

        ndk {
            abiFilters += listOf("arm64-v8a", "armeabi-v7a")
        }
    }

    val keystoreFile = System.getenv("ANDROID_KEYSTORE_FILE")?.let { file(it) }
    val keystorePassword = System.getenv("ANDROID_KEYSTORE_PASSWORD")
    val keyAlias = System.getenv("ANDROID_KEY_ALIAS")
    val keyPassword = System.getenv("ANDROID_KEY_PASSWORD")
    val requireSigning = project.hasProperty("requireSigning") || System.getenv("CI") == "true"

    signingConfigs {
        create("release") {
            if (keystoreFile != null && keystoreFile.exists() && !keystorePassword.isNullOrEmpty() && !keyAlias.isNullOrEmpty() && !keyPassword.isNullOrEmpty()) {
                storeFile = keystoreFile
                storePassword = keystorePassword
                this.keyAlias = keyAlias
                this.keyPassword = keyPassword
            } else if (requireSigning) {
                throw GradleException("Release signing credentials missing while requireSigning is active. Keystore file exists: ${keystoreFile?.exists() == true}")
            }
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = false
            proguardFiles(
                getDefaultProguardFile("proguard-android-optimize.txt"),
                "proguard-rules.pro"
            )
            signingConfig = signingConfigs.getByName("release")
        }
        debug {
            isMinifyEnabled = false
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    kotlinOptions {
        jvmTarget = "17"
    }

    packaging {
        resources {
            excludes += "/META-INF/{AL2.0,LGPL2.1}"
        }
    }
}

dependencies {
    compileOnly(files("libs/api-82.jar"))
    implementation("org.luckypray:dexkit:2.0.3")
    implementation("org.jetbrains.kotlinx:kotlinx-coroutines-android:1.8.1")
}
