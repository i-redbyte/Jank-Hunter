package io.jankhunter.plugin.ui

import com.intellij.testFramework.fixtures.BasePlatformTestCase
import com.intellij.ui.components.JBTextArea
import java.awt.Component
import java.awt.Container
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger
import javax.swing.AbstractButton
import javax.swing.JComponent
import javax.swing.JScrollPane
import javax.swing.SwingUtilities
import javax.swing.border.Border
import javax.swing.border.CompoundBorder
import javax.swing.border.TitledBorder

class JankHunterAdvancedPanelTest : BasePlatformTestCase() {
    fun testArtifactDiscoveryIsLazyAndRunsOnlyOnceAutomatically() {
        val calls = AtomicInteger()
        val started = CountDownLatch(1)
        val panel = JankHunterAdvancedPanel(
            project = project,
            onBack = {},
            onRun = {},
            onScorecard = {},
            artifactFinder = {
                calls.incrementAndGet()
                started.countDown()
                emptyList()
            },
        )
        try {
            assertEquals(0, calls.get())

            panel.ensureArtifactsLoaded()
            assertTrue(started.await(5, TimeUnit.SECONDS))
            panel.ensureArtifactsLoaded()

            assertEquals(1, calls.get())
        } finally {
            panel.dispose()
        }
    }

    fun testSimpleOptionsDoNotContainAdvancedInputs() {
        val options = JankHunterAdvancedOptions.SIMPLE

        assertTrue(options.artifactsDir.isBlank())
        assertTrue(options.mapping.isBlank())
        assertTrue(options.classGraph.isBlank())
        assertTrue(options.diagnostics.isBlank())
        assertTrue(options.diCatalog.isBlank())
        assertTrue(options.route.isBlank())
        assertTrue(options.screen.isBlank())
        assertTrue(options.owner.isBlank())
        assertTrue(options.className.isBlank())
        assertFalse(options.presentation)
    }

    fun testAnimationToggleIsNotShown() {
        val panel = JankHunterAdvancedPanel(
            project = project,
            onBack = {},
            onRun = {},
            onScorecard = {},
            artifactFinder = { emptyList() },
        )
        try {
            val labels = components(panel.component)
                .filterIsInstance<AbstractButton>()
                .mapNotNull(AbstractButton::getText)
                .toList()

            assertFalse(labels.any { it.contains("animated", ignoreCase = true) })
        } finally {
            panel.dispose()
        }
    }

    fun testDiagnosticsGroupKeepsTextAreasAtTheirPreferredHeight() {
        val panel = JankHunterAdvancedPanel(
            project = project,
            onBack = {},
            onRun = {},
            onScorecard = {},
            artifactFinder = { emptyList() },
        )
        try {
            val diagnosticsGroup = components(panel.component)
                .filterIsInstance<JComponent>()
                .first { borderTitle(it.border) == "Диагностика запуска" }
            assertEquals(diagnosticsGroup.preferredSize.height, diagnosticsGroup.maximumSize.height)

            val commandArea = components(panel.component)
                .filterIsInstance<JBTextArea>()
                .first { it.text.startsWith("Команда будет показана") }
            val commandPane = SwingUtilities.getAncestorOfClass(JScrollPane::class.java, commandArea) as JScrollPane
            val threeTextRows = commandArea.getFontMetrics(commandArea.font).height * 3
            assertTrue(commandPane.preferredSize.height >= threeTextRows)
        } finally {
            panel.dispose()
        }
    }

    fun testReportAndCompareModesExposeMatchingDirectoryActions() {
        val reportMode = AtomicInteger()
        val compareMode = AtomicInteger()
        val reportDirectory = AtomicInteger()
        val baselineDirectory = AtomicInteger()
        val candidateDirectory = AtomicInteger()
        val runs = AtomicInteger()
        val panel = JankHunterAdvancedPanel(
            project = project,
            onBack = {},
            onRun = runs::incrementAndGet,
            onScorecard = {},
            onSelectReport = reportMode::incrementAndGet,
            onSelectCompare = compareMode::incrementAndGet,
            onChooseReportDirectory = reportDirectory::incrementAndGet,
            onChooseBaselineDirectory = baselineDirectory::incrementAndGet,
            onChooseCandidateDirectory = candidateDirectory::incrementAndGet,
            artifactFinder = { emptyList() },
        )
        try {
            val buttons = components(panel.component)
                .filterIsInstance<AbstractButton>()
                .associateBy { it.text }
            val runButton = buttons.getValue("Сгенерировать отчёт")

            panel.setContext("Отчёт: 0 логов", compare = false)
            assertEquals("Сгенерировать отчёт", runButton.text)
            assertTrue(buttons.getValue("Выбрать папку логов…").isVisible)
            assertFalse(buttons.getValue("Выбрать baseline…").isVisible)
            assertFalse(buttons.getValue("Выбрать candidate…").isVisible)
            buttons.getValue("Сравнение").doClick()
            buttons.getValue("Выбрать папку логов…").doClick()
            runButton.doClick()

            panel.setContext("Сравнение: 0 baseline → 0 candidate", compare = true)
            assertEquals("Сравнить отчёты", runButton.text)
            assertFalse(buttons.getValue("Выбрать папку логов…").isVisible)
            assertTrue(buttons.getValue("Выбрать baseline…").isVisible)
            assertTrue(buttons.getValue("Выбрать candidate…").isVisible)
            buttons.getValue("Отчёт").doClick()
            buttons.getValue("Выбрать baseline…").doClick()
            buttons.getValue("Выбрать candidate…").doClick()
            runButton.doClick()

            assertEquals(1, reportMode.get())
            assertEquals(1, compareMode.get())
            assertEquals(1, reportDirectory.get())
            assertEquals(1, baselineDirectory.get())
            assertEquals(1, candidateDirectory.get())
            assertEquals(2, runs.get())
        } finally {
            panel.dispose()
        }
    }

    private fun components(component: Component): Sequence<Component> = sequence {
        yield(component)
        if (component is Container) {
            component.components.forEach { child -> yieldAll(components(child)) }
        }
    }

    private fun borderTitle(border: Border?): String? = when (border) {
        is TitledBorder -> border.title
        is CompoundBorder -> borderTitle(border.outsideBorder) ?: borderTitle(border.insideBorder)
        else -> null
    }
}
