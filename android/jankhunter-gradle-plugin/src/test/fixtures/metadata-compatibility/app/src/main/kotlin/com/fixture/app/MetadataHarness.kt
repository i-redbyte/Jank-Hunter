package com.fixture.app

class MetadataHarness {
    fun capture(value: Any): () -> Any = { value }

    fun answer(value: Int): Int = value + 1

    inline fun compute(flag: Boolean): Int = if (flag) 1 else 0
}
