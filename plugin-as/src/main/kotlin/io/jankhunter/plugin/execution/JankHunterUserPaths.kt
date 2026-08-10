package io.jankhunter.plugin.execution

import java.io.File

object JankHunterUserPaths {
    fun expandHome(raw: String): String {
        val value = raw.trim()
        if (value == "~") return homeDirectory().path
        val prefix = "~${File.separator}"
        val relative = when {
            value.startsWith(prefix) -> value.removePrefix(prefix)
            File.separatorChar != '/' && value.startsWith("~/") -> value.removePrefix("~/")
            else -> return value
        }
        return File(homeDirectory(), relative).path
    }

    internal fun homeDirectory(): File = resolveHomeDirectory(
        userHome = System.getProperty("user.home"),
        userDirectory = System.getProperty("user.dir"),
    )

    internal fun resolveHomeDirectory(userHome: String?, userDirectory: String?): File {
        val path = userHome?.trim()?.takeIf(String::isNotEmpty)
            ?: userDirectory?.trim()?.takeIf(String::isNotEmpty)
            ?: "."
        return File(path).toPath().toAbsolutePath().normalize().toFile()
    }
}
