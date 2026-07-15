package io.jankhunter.plugin.problems

import javax.swing.table.AbstractTableModel

internal class ProblemsTableModel : AbstractTableModel() {
    private var table: ProblemsTable = ProblemsTable(emptyList(), emptyList())

    fun setTable(next: ProblemsTable) {
        table = next
        fireTableStructureChanged()
    }

    fun rowAt(row: Int): Map<String, String> {
        if (row !in table.rows.indices) return emptyMap()
        return table.rows[row]
    }

    override fun getRowCount(): Int = table.rows.size

    override fun getColumnCount(): Int = table.columns.size

    override fun getColumnName(column: Int): String = table.columns.getOrElse(column) { "" }

    override fun getValueAt(rowIndex: Int, columnIndex: Int): Any =
        table.rows.getOrNull(rowIndex)?.get(table.columns.getOrElse(columnIndex) { "" }).orEmpty()
}
