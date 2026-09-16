package io.jankhunter.gradle

/** Stable method identity shared by instrumentation bytecode and artifact metadata. */
internal object OwnerIds {
    const val STABLE_ID_ALGORITHM =
        "fnv1a64-utf8(internal-class,NUL,method,NUL,descriptor);" +
            "offset=0xcbf29ce484222325;prime=0x100000001b3;v=1"
    const val STABLE_ID_ENCODING = "stable:0x%016x"

    fun readableOwner(className: String, methodName: String): String {
        return "${className.replace('/', '.')}.$methodName"
    }

    fun coroutineOwner(
        className: String,
        enclosingClassName: String? = null,
        enclosingMethodName: String? = null,
    ): String {
        if (enclosingClassName != null && enclosingMethodName != null) {
            if (enclosingMethodName == "invokeSuspend") return coroutineOwner(enclosingClassName)
            return readableOwner(enclosingClassName, enclosingMethodName)
        }
        val readableClass = className.replace('/', '.')
        val segments = readableClass.split('$')
        val functionIndex = segments.indexOfLast { segment ->
            segment.isNotEmpty() && !segment.first().isDigit() && segment !in GENERATED_NAME_SEGMENTS
        }
        if (functionIndex <= 0) return "${segments.first()}.coroutine"
        return "${segments.take(functionIndex).joinToString("$")}.${segments[functionIndex]}"
    }

    fun methodId(className: String, methodName: String, descriptor: String): Long {
        val internalClassName = className.replace('.', '/')
        return fnv1a64("$internalClassName\u0000$methodName\u0000$descriptor").toLong()
    }

    fun canonical(methodId: Long): String {
        return "stable:0x${methodId.toULong().toString(16).padStart(16, '0')}"
    }

    private fun fnv1a64(value: String): ULong {
        var hash = 0xcbf29ce484222325UL
        for (byte in value.encodeToByteArray()) {
            hash = hash xor byte.toUByte().toULong()
            hash *= 0x100000001b3UL
        }
        return hash
    }

    private val GENERATED_NAME_SEGMENTS = setOf("default", "inlined", "lambda", "special")
}
