package io.jankhunter.gradle

internal enum class AndroidReceiverInvocation {
    RECEIVE,
    GO_ASYNC,
    FINISH_ASYNC,
}

/** Exact BroadcastReceiver ABI surface; intentionally independent from Service instrumentation. */
internal object AndroidBroadcastReceiverInstrumentationPolicy {
    fun isReceiver(hierarchy: Set<String>): Boolean = ANDROID_RECEIVER in hierarchy

    fun callback(name: String, descriptor: String): AndroidReceiverInvocation? {
        return if (name == "onReceive" && descriptor == RECEIVE_DESCRIPTOR) {
            AndroidReceiverInvocation.RECEIVE
        } else {
            null
        }
    }

    fun invocation(
        owner: String,
        name: String,
        descriptor: String,
        ownerHierarchy: Set<String> = emptySet(),
    ): AndroidReceiverInvocation? {
        return when {
            (owner == ANDROID_RECEIVER || ANDROID_RECEIVER in ownerHierarchy) &&
                name == "goAsync" && descriptor == GO_ASYNC_DESCRIPTOR -> {
                AndroidReceiverInvocation.GO_ASYNC
            }
            owner == ANDROID_PENDING_RESULT && name == "finish" && descriptor == FINISH_DESCRIPTOR -> {
                AndroidReceiverInvocation.FINISH_ASYNC
            }
            else -> null
        }
    }

    fun needsOwnerHierarchy(name: String, descriptor: String): Boolean {
        return name == "goAsync" && descriptor == GO_ASYNC_DESCRIPTOR
    }

    private const val ANDROID_RECEIVER = "android/content/BroadcastReceiver"
    private const val ANDROID_PENDING_RESULT = "android/content/BroadcastReceiver\$PendingResult"
    private const val RECEIVE_DESCRIPTOR = "(Landroid/content/Context;Landroid/content/Intent;)V"
    private const val GO_ASYNC_DESCRIPTOR = "()Landroid/content/BroadcastReceiver\$PendingResult;"
    private const val FINISH_DESCRIPTOR = "()V"
}
