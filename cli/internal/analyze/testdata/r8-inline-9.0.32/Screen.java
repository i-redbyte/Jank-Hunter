package probe;
final class Screen {
  static void render(int value) { bind(value); }
  static void bind(int value) {
    if (value == 0) throw new IllegalStateException("fixture");
    System.out.println(value);
  }
}
