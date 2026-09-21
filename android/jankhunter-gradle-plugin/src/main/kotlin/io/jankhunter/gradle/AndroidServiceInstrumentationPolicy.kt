package io.jankhunter.gradle

import org.objectweb.asm.Opcodes

internal enum class AndroidServiceResultKind {
    NONE,
    INT,
    OBJECT,
}

internal enum class AndroidServiceCallback(
    val stage: Int,
    val intentArgument: Int,
    val resultKind: AndroidServiceResultKind,
) {
    CREATED(1, -1, AndroidServiceResultKind.NONE),
    START_COMMAND(2, 0, AndroidServiceResultKind.INT),
    BIND(3, 0, AndroidServiceResultKind.OBJECT),
    UNBIND(4, 0, AndroidServiceResultKind.INT),
    REBIND(5, 0, AndroidServiceResultKind.NONE),
    TASK_REMOVED(6, 0, AndroidServiceResultKind.NONE),
    DESTROYED(9, -1, AndroidServiceResultKind.NONE),
    TIMEOUT(10, -1, AndroidServiceResultKind.NONE),
    ;
}

internal data class AndroidServiceForegroundCall(
    val stage: Int,
    val serviceArgument: Int,
) {
    companion object {
        const val INVOCATION_RECEIVER = -1
    }
}

/** Exact Android API signatures only; similarly named application methods are never hooked. */
internal object AndroidServiceInstrumentationPolicy {
    fun isService(hierarchy: Set<String>): Boolean = ANDROID_SERVICE in hierarchy

    fun callback(name: String, descriptor: String): AndroidServiceCallback? {
        return CALLBACKS["$name$descriptor"]
    }

    fun foregroundCall(
        opcodeAndSource: Int,
        owner: String,
        name: String,
        descriptor: String,
        ownerHierarchy: Set<String>,
    ): AndroidServiceForegroundCall? {
        val opcode = opcodeAndSource and Opcodes.SOURCE_MASK.inv()
        val stage = when (name) {
            "startForeground" -> FOREGROUND_ENTERED
            "stopForeground" -> FOREGROUND_EXITED
            else -> return null
        }
        if (opcode == Opcodes.INVOKESTATIC && owner == ANDROIDX_SERVICE_COMPAT) {
            if (!isServiceCompatDescriptor(name, descriptor)) return null
            return AndroidServiceForegroundCall(stage, serviceArgument = 0)
        }
        if (opcode == Opcodes.INVOKESTATIC || ANDROID_SERVICE !in ownerHierarchy && owner != ANDROID_SERVICE) {
            return null
        }
        if (!isFrameworkDescriptor(name, descriptor)) return null
        return AndroidServiceForegroundCall(stage, AndroidServiceForegroundCall.INVOCATION_RECEIVER)
    }

    fun needsOwnerHierarchy(owner: String, name: String, descriptor: String): Boolean {
        return owner != ANDROID_SERVICE && owner != ANDROIDX_SERVICE_COMPAT &&
            isFrameworkDescriptor(name, descriptor)
    }

    private fun isServiceCompatDescriptor(name: String, descriptor: String): Boolean {
        return when (name) {
            "startForeground" -> descriptor == SERVICE_COMPAT_START_DESCRIPTOR
            "stopForeground" -> descriptor == SERVICE_COMPAT_STOP_DESCRIPTOR
            else -> false
        }
    }

    private fun isFrameworkDescriptor(name: String, descriptor: String): Boolean {
        return when (name) {
            "startForeground" -> descriptor == FRAMEWORK_START_DESCRIPTOR ||
                descriptor == FRAMEWORK_START_WITH_TYPE_DESCRIPTOR
            "stopForeground" -> descriptor == FRAMEWORK_STOP_BOOLEAN_DESCRIPTOR ||
                descriptor == FRAMEWORK_STOP_FLAGS_DESCRIPTOR
            else -> false
        }
    }

    private const val ANDROID_SERVICE = "android/app/Service"
    private const val ANDROIDX_SERVICE_COMPAT = "androidx/core/app/ServiceCompat"
    private const val FOREGROUND_ENTERED = 7
    private const val FOREGROUND_EXITED = 8
    private val CALLBACKS = mapOf(
        "onCreate()V" to AndroidServiceCallback.CREATED,
        "onStartCommand(Landroid/content/Intent;II)I" to AndroidServiceCallback.START_COMMAND,
        "onBind(Landroid/content/Intent;)Landroid/os/IBinder;" to AndroidServiceCallback.BIND,
        "onUnbind(Landroid/content/Intent;)Z" to AndroidServiceCallback.UNBIND,
        "onRebind(Landroid/content/Intent;)V" to AndroidServiceCallback.REBIND,
        "onTaskRemoved(Landroid/content/Intent;)V" to AndroidServiceCallback.TASK_REMOVED,
        "onDestroy()V" to AndroidServiceCallback.DESTROYED,
        "onTimeout(I)V" to AndroidServiceCallback.TIMEOUT,
        "onTimeout(II)V" to AndroidServiceCallback.TIMEOUT,
    )
    private const val FRAMEWORK_START_DESCRIPTOR = "(ILandroid/app/Notification;)V"
    private const val FRAMEWORK_START_WITH_TYPE_DESCRIPTOR = "(ILandroid/app/Notification;I)V"
    private const val FRAMEWORK_STOP_BOOLEAN_DESCRIPTOR = "(Z)V"
    private const val FRAMEWORK_STOP_FLAGS_DESCRIPTOR = "(I)V"
    private const val SERVICE_COMPAT_START_DESCRIPTOR =
        "(Landroid/app/Service;ILandroid/app/Notification;I)V"
    private const val SERVICE_COMPAT_STOP_DESCRIPTOR = "(Landroid/app/Service;I)V"
}
