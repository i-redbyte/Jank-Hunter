package fixture.javaabi;

public final class JavaTarget {
    public static int compute(int value) {
        try { return value + 1; }
        finally { if (value == Integer.MIN_VALUE) throw new IllegalArgumentException(); }
    }
    public static void fail(RuntimeException failure) { throw failure; }
}
