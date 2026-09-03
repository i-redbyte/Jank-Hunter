package io.jankhunter.gradle

internal enum class AndroidBinderInvocation {
    CLIENT_TRANSACTION,
    SERVER_TRANSACTION,
}

/** Exact Binder ABI matching and deterministic AIDL descriptor fallback. */
internal object AndroidBinderInstrumentationPolicy {
    fun isServer(hierarchy: Set<String>): Boolean = ANDROID_BINDER in hierarchy

    fun serverCallback(name: String, descriptor: String): AndroidBinderInvocation? {
        return if (name == "onTransact" && descriptor == TRANSACT_DESCRIPTOR) {
            AndroidBinderInvocation.SERVER_TRANSACTION
        } else {
            null
        }
    }

    fun clientInvocation(
        owner: String,
        name: String,
        descriptor: String,
        ownerHierarchy: Set<String> = emptySet(),
    ): AndroidBinderInvocation? {
        return if ((owner == ANDROID_IBINDER || ANDROID_IBINDER in ownerHierarchy) &&
            name == "transact" && descriptor == TRANSACT_DESCRIPTOR
        ) {
            AndroidBinderInvocation.CLIENT_TRANSACTION
        } else {
            null
        }
    }

    fun needsOwnerHierarchy(owner: String, name: String, descriptor: String): Boolean {
        return owner != ANDROID_IBINDER && name == "transact" && descriptor == TRANSACT_DESCRIPTOR
    }

    fun runtimeDescriptor(className: String, declaredDescriptor: String?): String? {
        if (!declaredDescriptor.isNullOrBlank()) return declaredDescriptor
        val suffixLength = when {
            className.endsWith(AIDL_PROXY_SUFFIX) -> AIDL_PROXY_SUFFIX.length
            className.endsWith(AIDL_STUB_SUFFIX) -> AIDL_STUB_SUFFIX.length
            else -> return null
        }
        val interfaceClass = className.substring(0, className.length - suffixLength)
        return interfaceClass.replace('/', '.')
    }

    fun runtimeMethod(className: String, methodName: String): String? {
        return if (className.endsWith(AIDL_PROXY_SUFFIX)) methodName else null
    }

    private const val ANDROID_BINDER = "android/os/Binder"
    private const val ANDROID_IBINDER = "android/os/IBinder"
    private const val AIDL_STUB_SUFFIX = "\$Stub"
    private const val AIDL_PROXY_SUFFIX = "\$Stub\$Proxy"
    private const val TRANSACT_DESCRIPTOR = "(ILandroid/os/Parcel;Landroid/os/Parcel;I)Z"
}
