package io.jankhunter.gradle

import org.objectweb.asm.Label
import org.objectweb.asm.Opcodes
import org.objectweb.asm.Type

/** Emits Android component and Binder hooks for one method transformation. */
internal class AndroidComponentMethodEmitter(
    private val visitor: JankHunterMethodVisitor,
    private val state: MethodInstrumentationState,
    private val className: String,
    private val methodName: String,
    private val serviceCallback: AndroidServiceCallback?,
    private val binderDescriptor: String?,
    private val recordServiceHookApplied: () -> Unit,
    private val recordReceiverHookApplied: () -> Unit,
    private val recordBinderHookApplied: () -> Unit,
) {
    fun serviceCallbackEnter(callback: AndroidServiceCallback) = with(visitor) {
        when (callback.resultKind) {
            AndroidServiceResultKind.INT -> {
                state.serviceResultCodeLocal = newLocal(Type.INT_TYPE)
                push(Int.MIN_VALUE)
                storeLocal(state.serviceResultCodeLocal)
            }
            AndroidServiceResultKind.OBJECT -> {
                state.serviceResultObjectLocal = newLocal(Type.getType(Any::class.java))
                visitInsn(Opcodes.ACONST_NULL)
                storeLocal(state.serviceResultObjectLocal)
            }
            AndroidServiceResultKind.NONE -> Unit
        }
        visitMethodInsn(Opcodes.INVOKESTATIC, ANDROID_HOOKS, "enterServiceCallback", "()J", false)
        state.serviceCallbackStartLocal = newLocal(Type.LONG_TYPE)
        storeLocal(state.serviceCallbackStartLocal)
        recordServiceHookApplied()
    }

    fun captureServiceCallbackResult(opcode: Int) = with(visitor) {
        when {
            state.serviceResultCodeLocal >= 0 && opcode == Opcodes.IRETURN -> {
                dup()
                storeLocal(state.serviceResultCodeLocal)
            }
            state.serviceResultObjectLocal >= 0 && opcode == Opcodes.ARETURN -> {
                dup()
                storeLocal(state.serviceResultObjectLocal)
            }
        }
    }

    fun serviceCallbackExit(failed: Boolean): Unit = with(visitor) {
        val callback = serviceCallback ?: return
        loadLocal(state.serviceCallbackStartLocal)
        loadThis()
        if (callback.intentArgument >= 0) loadArg(callback.intentArgument) else visitInsn(Opcodes.ACONST_NULL)
        visitLdcInsn(OwnerIds.methodId(className, COMPONENT_OWNER, SERVICE_KIND))
        visitLdcInsn(className.replace('/', '.'))
        push(callback.stage)
        if (state.serviceResultCodeLocal >= 0) loadLocal(state.serviceResultCodeLocal) else push(Int.MIN_VALUE)
        if (state.serviceResultObjectLocal >= 0) {
            loadLocal(state.serviceResultObjectLocal)
        } else {
            visitInsn(Opcodes.ACONST_NULL)
        }
        push(failed)
        visitMethodInsn(
            Opcodes.INVOKESTATIC,
            ANDROID_HOOKS,
            "exitServiceCallback",
            "(JLjava/lang/Object;Ljava/lang/Object;JLjava/lang/String;IILjava/lang/Object;Z)V",
            false,
        )
    }

    fun serviceForegroundCall(
        invocation: MethodInvocation,
        foregroundCall: AndroidServiceForegroundCall,
    ) = with(visitor) {
        val argumentTypes = Type.getArgumentTypes(invocation.descriptor)
        val argumentLocals = IntArray(argumentTypes.size)
        for (index in argumentTypes.indices.reversed()) {
            val type = argumentTypes[index]
            argumentLocals[index] = newLocal(type)
            storeLocal(argumentLocals[index], type)
        }
        val receiverLocal = if (
            foregroundCall.serviceArgument == AndroidServiceForegroundCall.INVOCATION_RECEIVER
        ) {
            newLocal(Type.getObjectType(invocation.owner)).also { storeLocal(it) }
        } else {
            -1
        }
        if (receiverLocal >= 0) loadLocal(receiverLocal)
        argumentTypes.indices.forEach { index -> loadLocal(argumentLocals[index], argumentTypes[index]) }
        emitOriginalInvocation(invocation)
        if (receiverLocal >= 0) {
            loadLocal(receiverLocal)
        } else {
            loadLocal(argumentLocals[foregroundCall.serviceArgument])
        }
        visitLdcInsn(OwnerIds.methodId(className, COMPONENT_OWNER, SERVICE_KIND))
        visitLdcInsn(className.replace('/', '.'))
        push(foregroundCall.stage)
        visitMethodInsn(
            Opcodes.INVOKESTATIC,
            ANDROID_HOOKS,
            "recordServiceForegroundTransition",
            "(Ljava/lang/Object;JLjava/lang/String;I)V",
            false,
        )
    }

    fun receiverCallbackEnter() = with(visitor) {
        state.receiverAsyncStartedLocal = newLocal(Type.BOOLEAN_TYPE)
        push(false)
        storeLocal(state.receiverAsyncStartedLocal)
        state.receiverPendingResultLocal = newLocal(Type.getType(Any::class.java))
        visitInsn(Opcodes.ACONST_NULL)
        storeLocal(state.receiverPendingResultLocal)
        loadThis()
        loadArg(RECEIVER_INTENT_ARGUMENT)
        visitLdcInsn(OwnerIds.methodId(className, COMPONENT_OWNER, RECEIVER_KIND))
        visitLdcInsn(className.replace('/', '.'))
        visitMethodInsn(
            Opcodes.INVOKESTATIC,
            ANDROID_HOOKS,
            "enterReceiverCallback",
            "(Ljava/lang/Object;Ljava/lang/Object;JLjava/lang/String;)J",
            false,
        )
        state.receiverCallbackStartLocal = newLocal(Type.LONG_TYPE)
        storeLocal(state.receiverCallbackStartLocal)
        recordReceiverHookApplied()
    }

    fun receiverCallbackExit(failed: Boolean) = with(visitor) {
        loadLocal(state.receiverCallbackStartLocal)
        loadThis()
        loadArg(RECEIVER_INTENT_ARGUMENT)
        visitLdcInsn(OwnerIds.methodId(className, COMPONENT_OWNER, RECEIVER_KIND))
        visitLdcInsn(className.replace('/', '.'))
        loadLocal(state.receiverAsyncStartedLocal)
        loadLocal(state.receiverPendingResultLocal)
        push(failed)
        visitMethodInsn(
            Opcodes.INVOKESTATIC,
            ANDROID_HOOKS,
            "exitReceiverCallback",
            "(JLjava/lang/Object;Ljava/lang/Object;JLjava/lang/String;ZLjava/lang/Object;Z)V",
            false,
        )
    }

    fun receiverGoAsync(invocation: MethodInvocation) = with(visitor) {
        emitOriginalInvocation(invocation)
        dup()
        storeLocal(state.receiverPendingResultLocal)
        loadLocal(state.receiverPendingResultLocal)
        loadThis()
        loadArg(RECEIVER_INTENT_ARGUMENT)
        loadLocal(state.receiverCallbackStartLocal)
        visitLdcInsn(OwnerIds.methodId(className, COMPONENT_OWNER, RECEIVER_KIND))
        visitLdcInsn(className.replace('/', '.'))
        visitMethodInsn(
            Opcodes.INVOKESTATIC,
            ANDROID_HOOKS,
            "registerReceiverAsync",
            "(Ljava/lang/Object;Ljava/lang/Object;Ljava/lang/Object;JJLjava/lang/String;)Z",
            false,
        )
        storeLocal(state.receiverAsyncStartedLocal)
    }

    fun receiverAsyncFinish(invocation: MethodInvocation) = with(visitor) {
        val pendingResultLocal = newLocal(Type.getObjectType(invocation.owner))
        storeLocal(pendingResultLocal)
        loadLocal(pendingResultLocal)
        emitOriginalInvocation(invocation)
        loadLocal(pendingResultLocal)
        visitMethodInsn(
            Opcodes.INVOKESTATIC,
            ANDROID_HOOKS,
            "finishReceiverAsync",
            "(Ljava/lang/Object;)V",
            false,
        )
    }

    fun binderServerEnter() = with(visitor) {
        state.binderServerResultLocal = newLocal(Type.BOOLEAN_TYPE)
        push(false)
        storeLocal(state.binderServerResultLocal)
        visitMethodInsn(Opcodes.INVOKESTATIC, ANDROID_HOOKS, "enterBinderServer", "()J", false)
        state.binderServerStartLocal = newLocal(Type.LONG_TYPE)
        storeLocal(state.binderServerStartLocal)
        recordBinderHookApplied()
    }

    fun captureBinderServerResult(opcode: Int): Unit = with(visitor) {
        if (opcode != Opcodes.IRETURN) return
        dup()
        storeLocal(state.binderServerResultLocal)
    }

    fun binderServerExit(throwableLocal: Int) = with(visitor) {
        loadLocal(state.binderServerStartLocal)
        pushNullableString(binderDescriptor)
        visitInsn(Opcodes.ACONST_NULL)
        loadArg(BINDER_CODE_ARGUMENT)
        loadArg(BINDER_FLAGS_ARGUMENT)
        loadLocal(state.binderServerResultLocal)
        if (throwableLocal >= 0) loadLocal(throwableLocal) else visitInsn(Opcodes.ACONST_NULL)
        visitMethodInsn(
            Opcodes.INVOKESTATIC,
            ANDROID_HOOKS,
            "exitBinderServer",
            "(JLjava/lang/String;Ljava/lang/String;IIZLjava/lang/Throwable;)V",
            false,
        )
    }

    fun binderClientTransaction(invocation: MethodInvocation) = with(visitor) {
        val argumentTypes = Type.getArgumentTypes(invocation.descriptor)
        val argumentLocals = IntArray(argumentTypes.size)
        for (index in argumentTypes.indices.reversed()) {
            val type = argumentTypes[index]
            argumentLocals[index] = newLocal(type)
            storeLocal(argumentLocals[index], type)
        }
        val receiverLocal = newLocal(Type.getObjectType(invocation.owner))
        storeLocal(receiverLocal)
        visitMethodInsn(Opcodes.INVOKESTATIC, ANDROID_HOOKS, "enterBinderClient", "()J", false)
        val tokenLocal = newLocal(Type.LONG_TYPE)
        storeLocal(tokenLocal)
        val start = Label()
        val end = Label()
        val handler = Label()
        val done = Label()
        visitLabel(start)
        loadLocal(receiverLocal)
        argumentTypes.indices.forEach { index -> loadLocal(argumentLocals[index], argumentTypes[index]) }
        emitOriginalInvocation(invocation)
        val resultLocal = newLocal(Type.BOOLEAN_TYPE)
        storeLocal(resultLocal)
        visitLabel(end)
        emitInvocationTryCatchBlock(start, end, handler, "java/lang/Throwable")
        binderClientExit(tokenLocal, argumentLocals, resultLocal, throwableLocal = -1)
        loadLocal(resultLocal)
        goTo(done)
        visitLabel(handler)
        val throwableLocal = newLocal(Type.getType(Throwable::class.java))
        storeLocal(throwableLocal)
        binderClientExit(tokenLocal, argumentLocals, resultLocal = -1, throwableLocal = throwableLocal)
        loadLocal(throwableLocal)
        visitInsn(Opcodes.ATHROW)
        visitLabel(done)
    }

    private fun binderClientExit(
        tokenLocal: Int,
        argumentLocals: IntArray,
        resultLocal: Int,
        throwableLocal: Int,
    ) = with(visitor) {
        loadLocal(tokenLocal)
        pushNullableString(binderDescriptor)
        pushNullableString(AndroidBinderInstrumentationPolicy.runtimeMethod(className, methodName))
        loadLocal(argumentLocals[BINDER_CODE_ARGUMENT])
        loadLocal(argumentLocals[BINDER_FLAGS_ARGUMENT])
        if (resultLocal >= 0) loadLocal(resultLocal) else push(false)
        if (throwableLocal >= 0) loadLocal(throwableLocal) else visitInsn(Opcodes.ACONST_NULL)
        visitMethodInsn(
            Opcodes.INVOKESTATIC,
            ANDROID_HOOKS,
            "exitBinderClient",
            "(JLjava/lang/String;Ljava/lang/String;IIZLjava/lang/Throwable;)V",
            false,
        )
    }

    private fun pushNullableString(value: String?) = with(visitor) {
        if (value == null) visitInsn(Opcodes.ACONST_NULL) else visitLdcInsn(value)
    }

    private companion object {
        const val ANDROID_HOOKS = "io/jankhunter/runtime/JankHunterAndroidHooks"
        const val COMPONENT_OWNER = "<android-component>"
        const val SERVICE_KIND = "service"
        const val RECEIVER_KIND = "receiver"
        const val RECEIVER_INTENT_ARGUMENT = 1
        const val BINDER_CODE_ARGUMENT = 0
        const val BINDER_FLAGS_ARGUMENT = 3
    }
}
