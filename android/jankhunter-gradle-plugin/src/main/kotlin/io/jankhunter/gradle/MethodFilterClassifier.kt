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
        classAccess: Int,
        methodAccess: Int,
        className: String,
        methodName: String,
        methodDescriptor: String,
        kotlinOrigin: KotlinMethodOrigin,
    ): MethodFilterDecision {
        if (mode == JankHunterMethodFilterMode.NONE) return NONE
        var categories: MutableList<String>? = null
        if (classAccess and Opcodes.ACC_SYNTHETIC != 0) categories = add(categories, ACC_SYNTHETIC_CLASS)
        if (isGeneratedHelperClass(className)) categories = add(categories, GENERATED_HELPER)
        if (kotlinOrigin.metadataKnown) {
            kotlinOrigin.generatedReason?.let { categories = add(categories, it) }
        } else {
            if (methodAccess and Opcodes.ACC_BRIDGE != 0) categories = add(categories, ACC_BRIDGE)
            if (methodAccess and Opcodes.ACC_SYNTHETIC != 0) categories = add(categories, ACC_SYNTHETIC)
            if (isJavaEnumHelper(classAccess, className, methodName, methodDescriptor)) {
                categories = add(categories, JAVA_ENUM_HELPER)
            }
            if (methodName.endsWith("-impl")) categories = add(categories, VALUE_CLASS_IMPL)
        }
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
            "\$\$SyntheticClass" in className ||
            "\$Lambda\$" in className ||
            "\$r8\$lambda\$" in className ||
            className.endsWith("\$DefaultImpls")
    }

    private fun isJavaEnumHelper(
        classAccess: Int,
        className: String,
        methodName: String,
        descriptor: String,
    ): Boolean {
        if (classAccess and Opcodes.ACC_ENUM == 0) return false
        val enumDescriptor = "L$className;"
        return methodName == "values" && descriptor == "()[$enumDescriptor" ||
            methodName == "valueOf" && descriptor == "(Ljava/lang/String;)$enumDescriptor"
    }

    private const val ACC_SYNTHETIC_CLASS = "acc_synthetic_class"
    private const val ACC_BRIDGE = "acc_bridge"
    private const val ACC_SYNTHETIC = "acc_synthetic"
    private const val GENERATED_HELPER = "generated_helper_class"
    private const val JAVA_ENUM_HELPER = "java_enum_helper"
    private const val VALUE_CLASS_IMPL = "value_class_impl"
}
