package io.jankhunter.gradle

import org.objectweb.asm.Opcodes

internal class MethodFilterDecision(
    val categories: List<String>,
    val exclusionReason: String?,
)

internal object MethodFilterClassifier {
    private val NONE = MethodFilterDecision(emptyList(), null)

    fun classify(
        mode: JankHunterMethodFilterMode,
        generatedHelperClass: Boolean,
        access: Int,
        methodName: String,
        descriptor: String,
    ): MethodFilterDecision {
        if (mode == JankHunterMethodFilterMode.DISABLED) return NONE
        var categories: MutableList<String>? = null
        if (access and Opcodes.ACC_BRIDGE != 0) categories = add(categories, ACC_BRIDGE)
        if (access and Opcodes.ACC_SYNTHETIC != 0) categories = add(categories, ACC_SYNTHETIC)
        if (methodName.startsWith("access\$")) categories = add(categories, ACCESS_HELPER)
        if (methodName.endsWith("\$default")) categories = add(categories, KOTLIN_DEFAULT)
        if (methodName.startsWith("\$r8\$lambda\$")) categories = add(categories, R8_LAMBDA)
        if (generatedHelperClass) categories = add(categories, GENERATED_HELPER)
        if (methodName.endsWith("-impl")) categories = add(categories, VALUE_CLASS_IMPL)
        if (isObjectMethod(methodName)) categories = add(categories, OBJECT_METHOD)
        if (isAccessorShape(methodName, descriptor)) categories = add(categories, ACCESSOR_SHAPE)
        val result = categories ?: return NONE
        return MethodFilterDecision(result, result.first())
    }

    private fun add(categories: MutableList<String>?, category: String): MutableList<String> {
        val result = categories ?: ArrayList(3)
        result += category
        return result
    }

    fun isGeneratedHelperClass(className: String): Boolean {
        return "\$\$ExternalSyntheticLambda" in className ||
            "\$Lambda\$" in className ||
            className.endsWith("\$DefaultImpls")
    }

    private fun isObjectMethod(name: String): Boolean {
        return name == "hashCode" || name == "equals" || name == "toString"
    }

    private fun isAccessorShape(name: String, descriptor: String): Boolean {
        val getterName = name.startsWith("get") || name.startsWith("is")
        val setterName = name.startsWith("set")
        if (!getterName && !setterName) return false
        val close = descriptor.indexOf(')')
        if (close < 1 || close + 1 >= descriptor.length) return false
        if (getterName) return close == 1 && descriptor[close + 1] != 'V'
        return descriptor[close + 1] == 'V' && hasSingleArgument(descriptor, close)
    }

    private fun hasSingleArgument(descriptor: String, close: Int): Boolean {
        var index = 1
        if (index >= close) return false
        while (index < close && descriptor[index] == '[') index++
        if (index >= close) return false
        index = if (descriptor[index] == 'L') {
            val end = descriptor.indexOf(';', index)
            if (end < 0 || end >= close) return false
            end + 1
        } else {
            index + 1
        }
        return index == close
    }

    private const val ACC_BRIDGE = "acc_bridge"
    private const val ACC_SYNTHETIC = "acc_synthetic"
    private const val ACCESS_HELPER = "access_helper"
    private const val KOTLIN_DEFAULT = "kotlin_default"
    private const val R8_LAMBDA = "r8_lambda"
    private const val GENERATED_HELPER = "generated_helper_class"
    private const val VALUE_CLASS_IMPL = "value_class_impl"
    private const val OBJECT_METHOD = "object_method"
    private const val ACCESSOR_SHAPE = "accessor_shape"
}
