package fixture.metadata

data class MetadataData(val value: Int) {
    fun declared(argument: Int = value): Int = argument + 1
}
